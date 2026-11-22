package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/drup/platform/internal/accesspolicy"
	"github.com/drup/platform/internal/adguard"
	"github.com/drup/platform/internal/auth"
	"github.com/drup/platform/internal/config"
	"github.com/drup/platform/internal/database/seeder"
	"github.com/drup/platform/internal/domain"
	"github.com/drup/platform/internal/ippanel"
	"github.com/drup/platform/internal/lifecycle"
	"github.com/drup/platform/internal/migrations"
	"github.com/drup/platform/internal/observability"
	"github.com/drup/platform/internal/payment"
	"github.com/drup/platform/internal/repository"
	"github.com/drup/platform/internal/requestbudget"
	"github.com/drup/platform/internal/tapin"
	"github.com/drup/platform/internal/worker"
	"github.com/google/uuid"
)

// enableCORS sets restricted origin headers according to the environment allowlist (fail-closed)
func enableCORS(next http.Handler, cfg *config.Config) http.Handler {
	allowedMap := make(map[string]bool)
	for _, origin := range cfg.AllowedOrigins {
		allowedMap[strings.ToLower(strings.TrimSpace(origin))] = true
	}
	isDev := seeder.IsDevEnvironment(cfg.Environment)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqOrigin := strings.TrimSpace(r.Header.Get("Origin"))
		matchedOrigin := ""

		if reqOrigin != "" {
			if allowedMap[strings.ToLower(reqOrigin)] {
				matchedOrigin = reqOrigin
			} else if isDev && (strings.HasPrefix(reqOrigin, "http://localhost:") || strings.HasPrefix(reqOrigin, "http://127.0.0.1:")) {
				matchedOrigin = reqOrigin
			}
		}

		if matchedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", matchedOrigin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Webhook-Secret, Idempotency-Key")
		} else if isDev && reqOrigin == "" {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Webhook-Secret, Idempotency-Key")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// tapinReencryptTotalFailure reports whether a re-encryption pass
// failed at the key level (SEC-01 fix, critic F-04): at least one
// non-empty token row exists, some rows failed, and NOT ONE row
// succeeded (neither re-encrypted nor already decryptable). That
// shape means the current key cannot read the stored tokens at all —
// booting would 500 every supplier profile that reads a token — so
// the caller fails startup instead of logging and continuing.
// Partial failures (some rows succeeded) return false: the key is
// fine and the failed rows stay untouched for manual handling.
func tapinReencryptTotalFailure(rep repository.ReencryptReport) bool {
	succeeded := rep.Encrypted + rep.AlreadyEncrypted
	nonEmpty := rep.Examined - rep.SkippedEmpty
	return rep.Failed > 0 && succeeded == 0 && nonEmpty > 0
}

func runAutoMigrations(ctx context.Context, db *sql.DB) error {
	dirs := []string{"migrations", "../migrations", "./backend/migrations", "/app/migrations"}
	migrationDir := os.Getenv("MIGRATIONS_DIR")
	if migrationDir == "" {
		for _, dir := range dirs {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				migrationDir = dir
				break
			}
		}
	}
	if migrationDir == "" {
		return errors.New("required migrations directory missing (fail-closed)")
	}
	if _, err := migrations.Run(ctx, db, migrations.Options{Dir: migrationDir, Logf: log.Printf}); err != nil {
		return errors.New("database migration validation failed (fail-closed); check migration directory, checksums and database availability")
	}
	log.Println("[INFO] Database schema verified and up-to-date")
	return nil
}

func main() {
	signalCtx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	if err := run(signalCtx); err != nil {
		log.Printf("[ERROR] %v", err)
		os.Exit(1)
	}
}

func run(signalCtx context.Context) (result error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("configuration error: %w", err)
	}
	// SEC-01: Tapin token cipher. In production TAPIN_TOKEN_KEY is
	// required (LoadConfig fails closed without it); in an explicitly
	// dev environment it is an ephemeral per-process key, also from
	// LoadConfig. The previous key is only a decryption fallback
	// during a rotation window (gate G-10).
	tapinCipher, err := tapin.NewTokenCipher(cfg.TapinTokenKey)
	if err != nil {
		return fmt.Errorf("tapin token cipher: %w", err)
	}
	tapinCipher.WithPreviousKey(cfg.TapinPreviousTokenKey)
	startupCtx, stopStartup := context.WithTimeout(signalCtx, cfg.StartupTimeout)
	defer stopStartup()
	process := &lifecycle.Process{Grace: cfg.ShutdownGrace}
	defer func() { result = errors.Join(result, process.Shutdown()) }()
	// PERF-03 G4: pool bounds come from the validated DB_POOL_* config
	// (defaults preserve the historical 25/10 pool).
	db, err := repository.NewPostgresDBWithPool(startupCtx, cfg.DatabaseURL, repository.PoolSettings{
		MaxOpenConns:    cfg.DBPoolMaxOpen,
		MaxIdleConns:    cfg.DBPoolMaxIdle,
		ConnMaxLifetime: cfg.DBPoolMaxLifetime,
		ConnMaxIdleTime: cfg.DBPoolMaxIdleTime,
	})
	if err != nil {
		return err
	}
	process.Closers = append(process.Closers, func(context.Context) error { return db.DB.Close() })
	log.Print("[INFO] Connected to PostgreSQL")
	if err = runAutoMigrations(startupCtx, db.DB); err != nil {
		return err
	}
	redisClient, err := repository.NewRedisClientContext(startupCtx, cfg.RedisURL)
	if err != nil {
		return err
	}
	process.Closers = append(process.Closers, func(context.Context) error { return redisClient.Client.Close() })
	log.Print("[INFO] Connected to Redis")
	state := newHealthState(db, redisClient, cfg)
	// OBS-01 D-06: drain 503s count as admission rejects.
	state.SetRejectReporter(observability.Default)
	process.State = state

	var catalogRepo *repository.CatalogRepository
	var storeRepo *repository.StoreRepository
	var orderRepo *repository.OrderRepository
	var wmsRepo *repository.WMSRepository
	var finRepo *repository.FinanceRepository
	var pricingRepo *repository.PricingRepository
	var adminRepo *repository.AdminRepository
	var supplierRepo *repository.SupplierRepository
	var tapinRepo *repository.TapinRepository
	var warehouseRepo *repository.WarehouseRepository
	var backgroundWorker *worker.BackgroundWorker

	var userRepo *repository.UserRepository
	var capabilityRepo *repository.CapabilityRepository
	var otpService *auth.OTPService
	var smsClient *ippanel.Client
	jwtService := auth.NewJWTService(cfg.JWTSecret)

	if redisClient != nil {
		otpService = auth.NewOTPService(redisClient, cfg.Environment)
		if cfg.IPPanelEnabled {
			smsClient = ippanel.New(cfg.IPPanelBaseURL, cfg.IPPanelAPIKey, cfg.IPPanelSender)
			otpService.ConfigureSMS(smsClient, cfg.IPPanelPatternCode, func(ctx context.Context, mobile, kind, status, providerID, message string) {
				if db == nil {
					return
				}
				masked := mobile
				if len(masked) > 7 {
					masked = masked[:4] + "****" + masked[len(masked)-3:]
				}
				_, _ = db.DB.ExecContext(ctx, `INSERT INTO sms_logs (id,mobile_masked,message_type,provider,provider_message_id,status,error) VALUES ($1,$2,$3,$4,$5,$6,$7)`, uuid.New(), masked, kind, "ippanel", providerID, status, message)
			})
		}
	}

	var shipmentRepo *repository.SupplierShipmentRepository
	var gatewayClient payment.PaymentGatewayClient
	if cfg.PaymentGatewayMode == "real" {
		gatewayClient = payment.NewRealShaparakGatewayClient(cfg.PSPTerminalID, cfg.PSPAPIKey)
	} else {
		gatewayClient = payment.NewMockPaymentGatewayClient(true)
	}
	capacityGateway, capacityCarrier, capacityErr := selectCapacityAdapters(os.Getenv, gatewayClient)
	if capacityErr != nil {
		return capacityErr
	}
	gatewayClient = capacityGateway
	if capacityCarrier != "" {
		tapinAPIBaseURL = capacityCarrier
	}
	gatewayClient = payment.ObserveGateway(gatewayClient)

	if db != nil {
		catalogRepo = repository.NewCatalogRepository(db.DB)
		storeRepo = repository.NewStoreRepository(db.DB)
		orderRepo = repository.NewOrderRepository(db.DB)
		wmsRepo = repository.NewWMSRepository(db.DB)
		finRepo = repository.NewFinanceRepository(db.DB)
		pricingRepo = repository.NewPricingRepository(db.DB)
		userRepo = repository.NewUserRepository(db.DB)
		capabilityRepo = repository.NewCapabilityRepository(db.DB)
		jwtService.SetCapabilityReader(capabilityRepo)
		if err := capabilityRepo.BackfillOwnershipCapabilities(startupCtx); err != nil {
			return errors.New("capability ownership bootstrap failed; check database availability")
		}
		adminRepo = repository.NewAdminRepository(db.DB)
		supplierRepo = repository.NewSupplierRepository(db.DB).WithTapinCipher(tapinCipher)
		tapinRepo = repository.NewTapinRepository(db.DB).WithTokenCipher(tapinCipher)
		warehouseRepo = repository.NewWarehouseRepository(db.DB)
		shipmentRepo = repository.NewSupplierShipmentRepository(db.DB)
		backgroundWorker = worker.NewBackgroundWorker(db.DB)
		backgroundWorker.SetOrderRepository(orderRepo)
		backgroundWorker.SetRedisClient(redisClient)
		backgroundWorker.SetFinanceRepository(finRepo)
		// FIN-013 §2.2: no PSP refund adapter exists until FIN-010
		// supplies one (RealShaparakGatewayClient still returns
		// ErrVerificationFailed). The refund-reconciliation job is
		// wired with an explicit nil provider — a recorded no-op per
		// tick — rather than left unwired: the queue, its gauges and
		// the admin trigger are live, and execution activates with a
		// one-line SetRefundProvider call when the adapter lands.
		// Never substitute a test double here.
		backgroundWorker.SetRefundProvider(nil)
		process.StopWorker = backgroundWorker.StopContext
		// SEC-01: encrypt legacy-plaintext supplier Tapin tokens in
		// place (gate G-10). Idempotent; a row that fails any check
		// is left untouched and reported, never dropped or lost.
		// A DB-level error fails startup. A per-row failure does
		// not - EXCEPT a total failure (critic F-04): if token rows
		// exist and NOT ONE could be re-encrypted, the key itself
		// is wrong (e.g. rotated without TAPIN_TOKEN_PREVIOUS_KEY)
		// and every supplier profile would 500 on decrypt, while
		// the deploy health gate (/api/health) would still pass.
		// Failing the boot rolls the deploy back to the old binary,
		// which still reads the untouched plaintext rows.
		// Skipped in dev when the Tapin key is ephemeral (critic
		// F-11): the key dies with the process, so encrypting dev
		// tokens under it would orphan them on the next restart.
		// Set TAPIN_TOKEN_KEY explicitly for a persistent dev key.
		if cfg.TapinTokenKeyExplicit {
			if rep, err := tapinRepo.ReencryptSupplierTapinTokens(startupCtx); err != nil {
				return fmt.Errorf("tapin token re-encryption: %w", err)
			} else if tapinReencryptTotalFailure(rep) {
				return fmt.Errorf("tapin token re-encryption: %d of %d non-empty rows failed and none succeeded \u2014 refusing to boot: TAPIN_TOKEN_KEY cannot decrypt the stored tokens (check TAPIN_TOKEN_PREVIOUS_KEY); rows left untouched", rep.Failed, rep.Examined-rep.SkippedEmpty)
			} else if rep.Failed > 0 {
				log.Printf("[ERROR] tapin token re-encryption: %d of %d rows failed verification (supplier ids: %v); rows left untouched for manual handling", rep.Failed, rep.Examined, rep.FailedIDs)
			} else if rep.Encrypted > 0 {
				log.Printf("[INFO] tapin token re-encryption: %d plaintext token(s) encrypted in place, %d already encrypted", rep.Encrypted, rep.AlreadyEncrypted)
			}
		} else {
			log.Printf("[INFO] tapin token re-encryption skipped: TAPIN_TOKEN_KEY is ephemeral (dev); set it explicitly to persist dev tokens")
		}

	}

	supplierHandlers := NewSupplierHandlers(
		supplierRepo,
		jwtService,
		cfg.NetworkVolumeIncentiveEnabled,
		capabilityRepo,
	).WithPricingRepository(pricingRepo).WithTapinRepository(tapinRepo)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/ad-network/report", RequireRoles(jwtService, "admin", "support_specialist")(func(w http.ResponseWriter, r *http.Request) { handleAdNetworkReport(w, r, storeRepo) }))
	mux.HandleFunc("/internal/domains/allow", func(w http.ResponseWriter, r *http.Request) { handleDomainPermission(w, r, storeRepo) })
	mux.HandleFunc("/api/v1/domain-store", func(w http.ResponseWriter, r *http.Request) { handleDomainStore(w, r, storeRepo) })
	mux.HandleFunc("/api/v1/seller/domains", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleOwnedDomains(w, r, storeRepo) }))
	mux.HandleFunc("/api/v1/seller/domains/", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleOwnedDomainAction(w, r, storeRepo) }))
	adGuard := adguard.New(cfg.JWTSecret)
	mux.HandleFunc("/api/v1/admin/operations", requireGlobalWarehouseRoles(jwtService, "admin", "logistics_specialist")(func(w http.ResponseWriter, r *http.Request) { handleOperationsOverview(w, r, db.DB) }))
	// G-03: correlation-ID lookup over the in-memory request ring.
	mux.HandleFunc("/api/v1/admin/observability/requests", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleAdminRequestLookup(w, r, observability.Default) }))
	mux.HandleFunc("/api/v1/admin/operations/qc/", requireGlobalWarehouseRoles(jwtService, "admin", "logistics_specialist")(func(w http.ResponseWriter, r *http.Request) { handleResolveOverdueQC(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/ads/challenge", func(w http.ResponseWriter, r *http.Request) { handleAdChallenge(w, r, adGuard, storeRepo) })
	mux.HandleFunc("/api/v1/admin/ippanel/settings", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleIPPanelSettings(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/ippanel/test", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleIPPanelTest(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/sms/credit", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleIPPanelCredit(w, r, smsClient) }))
	mux.HandleFunc("/api/v1/admin/sms/report", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleIPPanelRecipients(w, r, smsClient) }))
	mux.HandleFunc("/api/v1/admin/sms/logs", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if db == nil {
			http.Error(w, `{"error":"Database unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		rows, err := db.DB.QueryContext(r.Context(), `SELECT id,mobile_masked,message_type,provider_message_id,status,error,created_at FROM sms_logs ORDER BY created_at DESC LIMIT 100`)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		type entry struct {
			ID         string    `json:"id"`
			Mobile     string    `json:"mobile"`
			Type       string    `json:"type"`
			ProviderID string    `json:"providerMessageId"`
			Status     string    `json:"status"`
			Error      string    `json:"error,omitempty"`
			CreatedAt  time.Time `json:"createdAt"`
		}
		items := []entry{}
		for rows.Next() {
			var e entry
			if rows.Scan(&e.ID, &e.Mobile, &e.Type, &e.ProviderID, &e.Status, &e.Error, &e.CreatedAt) == nil {
				items = append(items, e)
			}
		}
		json.NewEncoder(w).Encode(items)
	}))

	// 0. Authentication & OTP Endpoints
	mux.HandleFunc("/api/v1/auth/otp/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleSendOTP(w, r, r.Context(), otpService)
	})

	mux.HandleFunc("/api/v1/auth/otp/verify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleVerifyOTP(w, r, r.Context(), otpService, userRepo, jwtService, supplierRepo, capabilityRepo, storeRepo)
	})

	// Password login is intentionally limited to the backend allowlisted super-admin account.
	mux.HandleFunc("/api/v1/auth/password/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || json.Unmarshal(body, &req) != nil || !accesspolicy.IsSuperAdminMobile(req.Username) {
			http.Error(w, `{"error":"Invalid username or password"}`, http.StatusUnauthorized)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		handlePasswordLogin(w, r, r.Context(), userRepo, jwtService, supplierRepo)
	})

	mux.HandleFunc("/api/v1/auth/supplier/register", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleRegister(w, r)
	})

	mux.HandleFunc("/api/v1/auth/me", AuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		claims, ok := r.Context().Value(userClaimsKey).(*auth.UserClaims)
		if !ok || claims == nil {
			http.Error(w, `{"error":"Unauthorized"}`, http.StatusUnauthorized)
			return
		}
		json.NewEncoder(w).Encode(claims)
	}))

	// 1. Health check
	mux.HandleFunc("/api/live", state.Live)
	mux.HandleFunc("/api/ready", state.Ready)
	mux.HandleFunc("/api/health", state.Ready)

	// REL-01 (G9): release contract endpoint - the commit this binary
	// was built from plus the contract version (routes hash) it
	// implements. The release validator and smoke suite compare this
	// against the staged frontend's dist/contract.json before any
	// traffic switch. See backend/cmd/server/version.go.
	mux.HandleFunc("/api/version", handleVersion)

	// 2. Categories Endpoint (Redis Cached)
	mux.HandleFunc("/api/v1/categories", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx := r.Context()
		cacheKey := "catalog:categories:all"
		var cached []domain.Category
		if redisClient != nil {
			if found, _ := redisClient.GetJSON(ctx, cacheKey, &cached); found {
				json.NewEncoder(w).Encode(cached)
				return
			}
		}

		if catalogRepo == nil {
			http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
			return
		}

		categories, err := catalogRepo.GetCategories(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if redisClient != nil {
			_ = redisClient.SetJSON(ctx, cacheKey, categories, 1*time.Hour)
		}
		json.NewEncoder(w).Encode(categories)
	})

	// 3. Niches Endpoint (Redis Cached)
	mux.HandleFunc("/api/v1/niches", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx := r.Context()
		categorySlug := r.URL.Query().Get("category")

		cacheKey := fmt.Sprintf("catalog:niches:%s", categorySlug)
		var cached []domain.Niche
		if redisClient != nil {
			if found, _ := redisClient.GetJSON(ctx, cacheKey, &cached); found {
				json.NewEncoder(w).Encode(cached)
				return
			}
		}

		if catalogRepo == nil {
			http.Error(w, `{"error":"Database not initialized"}`, http.StatusInternalServerError)
			return
		}

		niches, err := catalogRepo.GetNiches(ctx, categorySlug)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if redisClient != nil {
			_ = redisClient.SetJSON(ctx, cacheKey, niches, 1*time.Hour)
		}
		json.NewEncoder(w).Encode(niches)
	})

	// 4. Products Listing & Single Product Endpoint
	// OptionalAuthMiddleware: the routes stay public, but a valid
	// seller / supplier / admin session unlocks the supplier cost
	// fields on store views (see canSeeSupplierFields); anonymous
	// traffic is served exactly as before.
	mux.HandleFunc("/api/v1/products/", OptionalAuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx := r.Context()
		subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/products/")
		if subPath == "" {
			handleProductsList(w, r, ctx, catalogRepo, storeRepo, pricingRepo, supplierRepo)
			return
		}
		if strings.HasSuffix(subPath, "/reviews") && r.Method == http.MethodGet {
			productID := strings.TrimSuffix(subPath, "/reviews")
			productID = strings.Trim(productID, "/")
			handlePublicProductReviews(w, r, catalogRepo, productID)
			return
		}
		handleProductDetail(w, r, ctx, catalogRepo, storeRepo, pricingRepo, subPath, supplierRepo)
	}))

	mux.HandleFunc("/api/v1/products", OptionalAuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) {
		handleProductsList(w, r, r.Context(), catalogRepo, storeRepo, pricingRepo, supplierRepo)
	}))

	// Team 04 live parity (catalog): price bounds (D05), Digikala
	// facets (D40 decision: build on dk data), review dossier (D61).
	// These are the exact route lines added by Team 04; the supervisor
	// resolves main.go conflicts at integration.
	mux.HandleFunc("/api/v1/products/price-bounds", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		handleProductPriceBounds(w, r, r.Context(), catalogRepo, storeRepo, pricingRepo)
	})
	// PERF-01: distinct category slugs for one store's assortment (one
	// bounded query); replaces deriving them from the full catalog.
	mux.HandleFunc("/api/v1/products/store-categories", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		handleStoreCategorySlugs(w, r, r.Context(), catalogRepo, storeRepo)
	})
	mux.HandleFunc("/api/v1/brands", func(w http.ResponseWriter, r *http.Request) {
		handleBrandsList(w, r, catalogRepo)
	})
	mux.HandleFunc("/api/v1/categories/", func(w http.ResponseWriter, r *http.Request) {
		subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/categories/")
		handleCategoryFacets(w, r, catalogRepo, subPath)
	})
	mux.HandleFunc("/api/v1/admin/product-field-reviews/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleProductFieldReview(w, r, supplierRepo) }))
	mux.HandleFunc("/api/v1/admin/catalog/review/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/catalog/review/")
		handleCatalogReviewDossier(w, r, catalogRepo, id)
	}))

	// 5. Stores Listing & Registration
	mux.HandleFunc("/api/v1/seller/tickets", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleSellerTickets(w, r, storeRepo) }))
	// Existing identities may inspect their own stores while completing seller
	// onboarding; seller capability is granted only after store creation.
	mux.HandleFunc("/api/v1/stores/mine", RequireRoles(jwtService, "customer", "seller", "supplier", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleOwnedStoresList(w, r, r.Context(), storeRepo)
	}))
	registerStoreHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleRegisterStore(w, r, r.Context(), storeRepo, capabilityRepo)
	}
	mux.HandleFunc("/api/v1/stores/register", RequireRoles(jwtService, "customer", "seller", "supplier", "admin")(registerStoreHandler))

	mux.HandleFunc("/api/v1/stores/", handleStoresBlock(jwtService, storeRepo, finRepo, catalogRepo, pricingRepo, orderRepo))

	mux.HandleFunc("/api/v1/stores", func(w http.ResponseWriter, r *http.Request) {
		handleStoresList(w, r, r.Context(), storeRepo)
	})

	// 5. Ad Click Token Anti-fraud Validation (Redis)
	mux.HandleFunc("/api/v1/ads/events", func(w http.ResponseWriter, r *http.Request) { handleSellerAdEvent(w, r, storeRepo, adGuard) })
	mux.HandleFunc("/api/v1/ads/click", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"Use POST /api/v1/ads/events with a verified browser challenge"}`, http.StatusGone)
	})

	// 6. Orders Placement, Tracking, Delivery Confirmation
	mux.HandleFunc("/api/v1/orders/track/", func(w http.ResponseWriter, r *http.Request) {
		code := strings.TrimPrefix(r.URL.Path, "/api/v1/orders/track/")
		handleTrackOrder(w, r, r.Context(), code, orderRepo)
	})

	confirmDeliveryHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		idOrNumber := strings.TrimPrefix(r.URL.Path, "/api/v1/orders/confirm-delivery/")
		handleConfirmDelivery(w, r, r.Context(), idOrNumber, orderRepo)
	}
	mux.HandleFunc("/api/v1/orders/confirm-delivery/", RequireRoles(jwtService, "customer")(confirmDeliveryHandler))

	mux.HandleFunc("/api/v1/checkout-attempts/", RequireRoles(jwtService, "customer", "seller", "admin")(func(w http.ResponseWriter, r *http.Request) { handleCheckoutAttempt(w, r, orderRepo) }))

	ordersHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleCreateOrder(w, r, r.Context(), orderRepo, catalogRepo, storeRepo, shipmentRepo, pricingRepo, supplierRepo, db.DB)
			return
		}
		if r.Method == http.MethodGet {
			handleListOrders(w, r, r.Context(), orderRepo)
			return
		}
		writeMethodNotAllowed(w)
	}
	mux.HandleFunc("/api/v1/orders", RequireRoles(jwtService, "customer", "seller", "admin")(ordersHandler))

	// FIX 5 (Team 03 G1): live pre-checkout shipping quote. The
	// warehouse carrier tariff (/warehouse/carriers/quote) is a
	// different, warehouse-role route and is kept as-is.
	mux.HandleFunc("/api/v1/shipping/quote", RequireRoles(jwtService, "customer", "seller", "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleShippingQuote(w, r, r.Context(), catalogRepo, supplierRepo, db.DB)
	}))

	// Order Tracking by Order ID (GET /api/v1/orders/:orderId/tracking)
	mux.HandleFunc("/api/v1/orders/", RequireRoles(jwtService, "customer", "seller", "admin", "supplier")(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/tracking") && r.Method == http.MethodGet {
			sub := strings.TrimPrefix(r.URL.Path, "/api/v1/orders/")
			orderID := strings.TrimSuffix(sub, "/tracking")
			orderID = strings.Trim(orderID, "/")
			handleGetOrderTracking(w, r, r.Context(), orderID, orderRepo, shipmentRepo)
			return
		}
		// Team 03 (D19/D64): order lifecycle actions and shipping info.
		// TODO(U01) for cancel/return/return-quote/dispute patterns;
		// /shipping is the confirmed D64 pattern.
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/orders/")
		if strings.HasSuffix(sub, "/shipping") && r.Method == http.MethodGet {
			orderID := strings.Trim(strings.TrimSuffix(sub, "/shipping"), "/")
			handleOrderShipping(w, r, r.Context(), orderID, orderRepo, shipmentRepo)
			return
		}
		if strings.HasSuffix(sub, "/cancel") && r.Method == http.MethodPost {
			orderID := strings.Trim(strings.TrimSuffix(sub, "/cancel"), "/")
			handleOrderCancel(w, r, r.Context(), orderID, orderRepo)
			return
		}
		if strings.HasSuffix(sub, "/return-quote") && r.Method == http.MethodPost {
			orderID := strings.Trim(strings.TrimSuffix(sub, "/return-quote"), "/")
			handleOrderReturnQuote(w, r, r.Context(), orderID, orderRepo, catalogRepo, db.DB)
			return
		}
		if strings.HasSuffix(sub, "/return") && r.Method == http.MethodPost {
			orderID := strings.Trim(strings.TrimSuffix(sub, "/return"), "/")
			handleOrderReturn(w, r, r.Context(), orderID, orderRepo)
			return
		}
		if strings.HasSuffix(sub, "/dispute") && r.Method == http.MethodPost {
			orderID := strings.Trim(strings.TrimSuffix(sub, "/dispute"), "/")
			handleOrderDispute(w, r, r.Context(), orderID, finRepo, orderRepo)
			return
		}
		http.NotFound(w, r)
	}))

	// Team 03 (D36): customer returns collection and item actions.
	// GET /api/v1/returns answers in live (401 without a token, versus
	// 404 for an unregistered route). POST /api/v1/returns/:id/cancel
	// is named by migration 202610190005; the other action patterns
	// follow the same shape with TODO(U01) in their handler comment.
	mux.HandleFunc("/api/v1/returns", RequireRoles(jwtService, "customer", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleListCustomerReturns(w, r, r.Context(), orderRepo)
			return
		}
		if r.Method == http.MethodPost {
			handleCreateCustomerReturnBody(w, r, r.Context(), orderRepo)
			return
		}
		writeMethodNotAllowed(w)
	}))
	mux.HandleFunc("/api/v1/returns/", requireWarehouseCityRoles(jwtService, "customer", "admin", "warehouse_staff", "logistics_specialist", "finance_specialist", "support_specialist")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/returns/")
		for _, action := range []string{"approve", "reject", "receive", "refund", "cancel", "ship"} {
			if strings.HasSuffix(sub, "/"+action) && r.Method == http.MethodPost {
				id := strings.Trim(strings.TrimSuffix(sub, "/"+action), "/")
				handleCustomerReturnAction(w, r, r.Context(), id, action, orderRepo)
				return
			}
		}
		if r.Method == http.MethodGet {
			handleGetCustomerReturn(w, r, r.Context(), strings.Trim(sub, "/"), orderRepo)
			return
		}
		http.NotFound(w, r)
	}))

	// 7. Warehouse WMS & Fulfillment Endpoints
	warehouseRoles := []string{"warehouse_staff", "logistics_specialist", "admin"}
	mux.HandleFunc("/api/v1/warehouse/customer-returns/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) { handleCustomerReturnBinEntry(w, r, warehouseRepo) }))

	mux.HandleFunc("/api/v1/warehouse/bins", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetBins(w, r, r.Context(), wmsRepo)
	}))
	mux.HandleFunc("/api/v1/warehouse/overview", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleGetWarehouseOverview(w, r, r.Context(), wmsRepo)
	}))
	// Warehouse notification inbox (Phase 2, W2): outbox rows
	// addressed to the calling warehouse staff user (inbound /
	// return-QC notices from W1). Same envelope + shared helpers
	// as the supplier inbox; same warehouseRoles gate as the other
	// warehouse routes.
	mux.HandleFunc("/api/v1/warehouse/notifications", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleWarehouseNotifications(w, r, db.DB)
	}))
	mux.HandleFunc("/api/v1/warehouse/notifications/read", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleWarehouseNotificationsRead(w, r, db.DB)
	}))
	mux.HandleFunc("/api/v1/warehouse/inventory/reconciliation-report", requireGlobalWarehouseRoles(jwtService, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleGetHistoricalInventoryReport(w, r, r.Context(), supplierRepo)
	}))
	mux.HandleFunc("/api/v1/warehouse/inventory/reconciliation-reviews/", requireGlobalWarehouseRoles(jwtService, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleReviewHistoricalInventory(w, r, r.Context(), supplierRepo, adminRepo)
	}))
	mux.HandleFunc("/api/v1/warehouse/bins/", requireGlobalWarehouseRoles(jwtService, "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/position") {
			handleBinPosition(w, r, warehouseRepo)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/capacity") && (r.Method == http.MethodPut || r.Method == http.MethodPost) {
			binCode := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/bins/"), "/capacity")
			handleUpdateBinCapacity(w, r, r.Context(), binCode, wmsRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/warehouse/inbounds/pending", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if !t01MethodOnly(w, r, http.MethodGet) {
			return
		}
		handleGetPendingInbounds(w, r, r.Context(), warehouseRepo)
	}))

	// Suppliers use the membership-scoped /api/v1/supplier/inbounds routes.
	mux.HandleFunc("/api/v1/warehouse/inbounds/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/inbounds/")
		if strings.HasSuffix(sub, "/start-receiving") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(sub, "/start-receiving")
			handleStartReceivingInbound(w, r, ctx, id, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/scan-item") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(sub, "/scan-item")
			handleScanInboundItem(w, r, ctx, id, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/complete") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(sub, "/complete")
			handleCompleteInbound(w, r, ctx, id, warehouseRepo)
			return
		}
		if sub == "" {
			handleGetInbounds(w, r, ctx, wmsRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/warehouse/inbounds", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetInbounds(w, r, r.Context(), wmsRepo)
	}))

	mux.HandleFunc("/api/v1/warehouse/qc/queue", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetQCQueue(w, r, r.Context(), warehouseRepo)
	}))

	mux.HandleFunc("/api/v1/warehouse/qc/discrepancy/create", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleCreateQCDiscrepancy(w, r, r.Context(), warehouseRepo)
	}))

	mux.HandleFunc("/api/v1/warehouse/qc/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/qc/")
		if strings.HasSuffix(sub, "/pass") && r.Method == http.MethodPost {
			prodID := strings.TrimSuffix(sub, "/pass")
			handlePassQCItem(w, r, ctx, prodID, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/fail") && r.Method == http.MethodPost {
			prodID := strings.TrimSuffix(sub, "/fail")
			handleFailQCItem(w, r, ctx, prodID, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/complete") && r.Method == http.MethodPost {
			prodID := strings.TrimSuffix(sub, "/complete")
			handleCompleteQC(w, r, ctx, prodID, warehouseRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/warehouse/putaway/queue", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetPutawayQueue(w, r, r.Context(), warehouseRepo)
	}))

	mux.HandleFunc("/api/v1/warehouse/putaway/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/putaway/")
		if strings.HasSuffix(sub, "/assign") && r.Method == http.MethodPost {
			prodID := strings.TrimSuffix(sub, "/assign")
			handleAssignPutaway(w, r, ctx, prodID, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/confirm") && r.Method == http.MethodPost {
			taskID := strings.TrimSuffix(sub, "/confirm")
			handleConfirmPutaway(w, r, ctx, taskID, warehouseRepo)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/warehouse/putaway/tasks/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/putaway/tasks/")
		if strings.HasSuffix(sub, "/suggestions") {
			handleBinSuggestions(w, r, warehouseRepo, strings.TrimSuffix(sub, "/suggestions"))
			return
		}
		if strings.HasSuffix(sub, "/assign") && r.Method == http.MethodPost {
			handleAssignPutawayTask(w, r, r.Context(), strings.TrimSuffix(sub, "/assign"), warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/confirm") && r.Method == http.MethodPost {
			handleConfirmPutaway(w, r, r.Context(), strings.TrimSuffix(sub, "/confirm"), warehouseRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/warehouse/returns/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if !requireUnscopedWarehouseWork(w, r) {
			return
		}
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/returns/")
		if strings.HasSuffix(sub, "/correct-charge") && r.Method == http.MethodPost {
			parts := strings.Split(strings.TrimSuffix(strings.Trim(sub, "/"), "/correct-charge"), "/parcels/")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" && !strings.Contains(parts[0], "/") && !strings.Contains(parts[1], "/") {
				handleCorrectReturnCharge(w, r, r.Context(), parts[0], parts[1], supplierRepo)
				return
			}
		}
		if strings.HasSuffix(sub, "/confirm-charge") && r.Method == http.MethodPost {
			parts := strings.Split(strings.TrimSuffix(strings.Trim(sub, "/"), "/confirm-charge"), "/parcels/")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				handleConfirmReturnCharge(w, r, r.Context(), parts[0], parts[1], supplierRepo)
				return
			}
		}
		if strings.HasSuffix(sub, "/review") && r.Method == http.MethodPost {
			handleReviewSupplierReturn(w, r, r.Context(), strings.TrimSuffix(sub, "/review"), supplierRepo)
			return
		}
		if strings.HasSuffix(sub, "/handover") && r.Method == http.MethodPost {
			handleHandoverReturn(w, r, r.Context(), strings.TrimSuffix(sub, "/handover"), supplierRepo)
			return
		}
		if strings.HasSuffix(sub, "/receive") && r.Method == http.MethodPost {
			handleReceiveReturn(w, r, r.Context(), strings.TrimSuffix(sub, "/receive"), supplierRepo)
			return
		}
		if strings.HasSuffix(sub, "/inspect") && r.Method == http.MethodPost {
			handleInspectReturn(w, r, r.Context(), strings.TrimSuffix(sub, "/inspect"), supplierRepo)
			return
		}
		if strings.HasSuffix(sub, "/parcels") && r.Method == http.MethodPost {
			handleCreateReturnParcel(w, r, r.Context(), strings.TrimSuffix(sub, "/parcels"), supplierRepo)
			return
		}
		if strings.HasSuffix(sub, "/parcels") && r.Method == http.MethodGet {
			handleGetReturnParcels(w, r, r.Context(), strings.TrimSuffix(sub, "/parcels"), supplierRepo)
			return
		}
		if strings.HasSuffix(sub, "/financial-events") && r.Method == http.MethodGet {
			handleGetReturnFinancialEvents(w, r, r.Context(), strings.TrimSuffix(sub, "/financial-events"), supplierRepo)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/warehouse/returns", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if !requireUnscopedWarehouseWork(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			handleGetSupplierReturns(w, r, r.Context(), supplierRepo)
			return
		}
		writeMethodNotAllowed(w)
	}))

	mux.HandleFunc("/api/v1/warehouse/products/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if !requireUnscopedWarehouseWork(w, r) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/measurements") && r.Method == http.MethodPost {
			productID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/products/"), "/measurements")
			handleRecordProductMeasurement(w, r, r.Context(), productID, supplierRepo)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/warehouse/carriers/quote", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if !requireUnscopedWarehouseWork(w, r) {
			return
		}
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleCarrierTariffQuote(w, r, r.Context(), db.DB)
	}))
	mux.HandleFunc("/api/v1/warehouse/carriers", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetCarrierOptions(w, r, r.Context(), supplierRepo)
			return
		}
		writeMethodNotAllowed(w)
	}))

	mux.HandleFunc("/api/v1/warehouse/orders/ready-to-pick", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetOrdersReadyToPick(w, r, r.Context(), warehouseRepo)
	}))

	mux.HandleFunc("/api/v1/warehouse/orders/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/orders/")
		if strings.HasSuffix(sub, "/start-picking") && r.Method == http.MethodPost {
			ordID := strings.TrimSuffix(sub, "/start-picking")
			handleStartPickingOrder(w, r, ctx, ordID, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/pick-item") && r.Method == http.MethodPost {
			ordID := strings.TrimSuffix(sub, "/pick-item")
			handlePickOrderItem(w, r, ctx, ordID, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/complete-picking") && r.Method == http.MethodPost {
			ordID := strings.TrimSuffix(sub, "/complete-picking")
			handleCompletePickingOrder(w, r, ctx, ordID, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/pack") && r.Method == http.MethodPost {
			ordID := strings.TrimSuffix(sub, "/pack")
			handlePackOrder(w, r, ctx, ordID, warehouseRepo)
			return
		}
		if strings.HasSuffix(sub, "/ship") && r.Method == http.MethodPost {
			ordID := strings.TrimSuffix(sub, "/ship")
			handleShipOrder(w, r, ctx, ordID, warehouseRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/warehouse/manifests/", requireWarehouseCityRoles(jwtService, "warehouse_staff", "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if !requireUnscopedWarehouseWork(w, r) {
			return
		}
		ctx := r.Context()
		subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/manifests/")
		if strings.HasSuffix(subPath, "/dispatch") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(subPath, "/dispatch")
			handleDispatchManifest(w, r, ctx, id, wmsRepo)
			return
		}
		handleGetManifests(w, r, ctx, wmsRepo)
	}))

	mux.HandleFunc("/api/v1/warehouse/manifests", requireWarehouseCityRoles(jwtService, "warehouse_staff", "logistics_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if !requireUnscopedWarehouseWork(w, r) {
			return
		}
		handleGetManifests(w, r, r.Context(), wmsRepo)
	}))

	// 8. Finance & Ledger Endpoints
	financeRoles := []string{"finance", "finance_specialist", "admin"}

	mux.HandleFunc("/api/v1/finance/ledger", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetFilteredLedger(w, r, r.Context(), finRepo)
	}))

	mux.HandleFunc("/api/v1/finance/stores/", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/finance/stores/")
		if strings.HasSuffix(sub, "/balance") {
			storeID := strings.TrimSuffix(sub, "/balance")
			handleGetStoreBalance(w, r, ctx, storeID, finRepo)
			return
		}
		// Statement months are Asia/Tehran calendar months (FIN-009
		// Q-02: TEHRAN, owner decision Nov 19 — supersedes the FIN-001
		// UTC month); ?month=YYYY-MM defaults to the current Tehran
		// month, ?limit= / ?cursor= page the entries (FIN-009
		// D-03/D-04).
		if strings.HasSuffix(sub, "/statement") {
			storeID := strings.TrimSuffix(sub, "/statement")
			handleGetStoreStatement(w, r, ctx, storeID, finRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/finance/settlements/pending", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetPendingSettlements(w, r, r.Context(), finRepo)
	}))

	mux.HandleFunc("/api/v1/finance/settlements/", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/finance/settlements/")
		if strings.HasSuffix(sub, "/approve") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(sub, "/approve")
			handleReviewSettlement(w, r, ctx, id, "approved", finRepo)
			return
		}
		if strings.HasSuffix(sub, "/reject") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(sub, "/reject")
			handleReviewSettlement(w, r, ctx, id, "rejected", finRepo)
			return
		}
		// Team 02 (D32): confirming a manual transfer is the only step
		// that writes the ledger debit; approval alone does not.
		if strings.HasSuffix(sub, "/transfer-confirmed") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(sub, "/transfer-confirmed")
			handleConfirmManualTransfer(w, r, ctx, id, finRepo)
			return
		}
		if r.Method == http.MethodGet {
			handleGetSettlementDetails(w, r, ctx, sub, finRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/finance/disputes/create", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleCreateManualDispute(w, r, r.Context(), finRepo)
	}))

	mux.HandleFunc("/api/v1/finance/disputes/", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/finance/disputes/")
		if strings.HasSuffix(subPath, "/resolve") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(subPath, "/resolve")
			handleResolveDispute(w, r, ctx, id, finRepo, orderRepo)
			return
		}
		handleGetDisputes(w, r, ctx, finRepo)
	}))

	mux.HandleFunc("/api/v1/finance/disputes", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleCreateDispute(w, r, r.Context(), finRepo)
			return
		}
		handleGetDisputes(w, r, r.Context(), finRepo)
	}))

	mux.HandleFunc("/api/v1/finance/reports/revenue", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleGetRevenueReport(w, r, r.Context(), finRepo)
	}))

	// 9. Delivery Disputes & Arbitration
	disputeReadWriteRoles := []string{"finance_specialist", "support_specialist", "logistics_specialist", "admin"}
	disputeResolveRoles := []string{"finance_specialist", "support_specialist", "admin"}
	mux.HandleFunc("/api/v1/disputes/", RequireRoles(jwtService, disputeReadWriteRoles...)(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/disputes/")
		if strings.HasSuffix(subPath, "/resolve") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(subPath, "/resolve")
			RequireRoles(jwtService, disputeResolveRoles...)(func(w http.ResponseWriter, r *http.Request) {
				handleResolveDispute(w, r, r.Context(), id, finRepo, orderRepo)
			})(w, r)
			return
		}
		handleGetDisputes(w, r, ctx, finRepo)
	}))

	mux.HandleFunc("/api/v1/disputes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			RequireRoles(jwtService, "customer", "support_specialist", "finance_specialist", "admin")(func(w http.ResponseWriter, r *http.Request) {
				handleCreateDispute(w, r, r.Context(), finRepo)
			})(w, r)
			return
		}
		RequireRoles(jwtService, disputeReadWriteRoles...)(func(w http.ResponseWriter, r *http.Request) {
			handleGetDisputes(w, r, r.Context(), finRepo)
		})(w, r)
	})

	// 10. Shaparak Payment Gateway Simulator & Sheba Bank Validator
	mux.HandleFunc("/api/v1/payments/shaparak/token", RequireRoles(jwtService, "customer", "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleShaparakToken(w, r, r.Context(), orderRepo, catalogRepo, redisClient)
	}))

	mux.HandleFunc("/api/v1/payments/shaparak/callback", func(w http.ResponseWriter, r *http.Request) {
		HandleShaparakCallbackHTTP(w, r, cfg.Environment, orderRepo, redisClient, gatewayClient)
	})

	mux.HandleFunc("/api/v1/bank/verify-sheba", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleVerifySheba(w, r)
	})

	// 11. Centralized Approvals Inbox & Audit Logs
	mux.HandleFunc("/api/v1/admin/approvals/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		subPath := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/approvals/")
		if strings.HasSuffix(subPath, "/review") && r.Method == http.MethodPost {
			id := strings.TrimSuffix(subPath, "/review")
			handleReviewApproval(w, r, ctx, id, adminRepo, supplierRepo)
			return
		}
		handleGetApprovals(w, r, ctx, adminRepo)
	}))

	mux.HandleFunc("/api/v1/admin/approvals", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleCreateApproval(w, r, r.Context(), adminRepo)
			return
		}
		handleGetApprovals(w, r, r.Context(), adminRepo)
	}))

	mux.HandleFunc("/api/v1/admin/audit-logs", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			handleCreateAuditLog(w, r, r.Context(), adminRepo)
			return
		}
		handleGetAuditLogs(w, r, r.Context(), adminRepo)
	}))

	mux.HandleFunc("/api/v1/admin/users", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleGetAdminUsers(w, r, r.Context(), adminRepo)
	}))

	mux.HandleFunc("/api/v1/admin/users/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/")
		if strings.HasSuffix(sub, "/block") && r.Method == http.MethodPost {
			userID := strings.TrimSuffix(sub, "/block")
			handleSetUserActiveStatus(w, r, ctx, userID, false, adminRepo)
			return
		}
		if strings.HasSuffix(sub, "/unblock") && r.Method == http.MethodPost {
			userID := strings.TrimSuffix(sub, "/unblock")
			handleSetUserActiveStatus(w, r, ctx, userID, true, adminRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/admin/stores/pending", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleGetPendingStores(w, r, r.Context(), adminRepo)
	}))

	mux.HandleFunc("/api/v1/admin/stores/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/stores/")
		if strings.HasSuffix(sub, "/approve") && r.Method == http.MethodPost {
			storeID := strings.TrimSuffix(sub, "/approve")
			handleReviewStore(w, r, ctx, storeID, "active", adminRepo)
			return
		}
		if strings.HasSuffix(sub, "/reject") && r.Method == http.MethodPost {
			storeID := strings.TrimSuffix(sub, "/reject")
			handleReviewStore(w, r, ctx, storeID, "rejected", adminRepo)
			return
		}
		if strings.HasSuffix(sub, "/lock-margin") && r.Method == http.MethodPost {
			storeID := strings.TrimSuffix(sub, "/lock-margin")
			handleLockStoreMargin(w, r, ctx, storeID, adminRepo)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/admin/products/proposals", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleGetProposals(w, r)
	}))

	// Team 02 live-parity pricing settings (D14). D20: admin/settings/base-fee
	// was deleted in live and is deliberately NOT registered here.
	// D30: there is no absolute fee-override WRITE route in live either;
	// fee overrides exist only as the historical read path below.
	mux.HandleFunc("/api/v1/admin/settings/pricing", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminPricingSettings(w, r, pricingRepo)
	}))

	// Gap 11: price-increase governance — admin review queue, per-request
	// review, and per-product exception grants (migrations 202610310003 +
	// 202610310006).
	mux.HandleFunc("/api/v1/admin/price-increases", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminPriceIncreases(w, r)
	}))

	mux.HandleFunc("/api/v1/admin/price-increases/{id}/review", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminReviewPriceIncrease(w, r, r.PathValue("id"))
	}))

	mux.HandleFunc("/api/v1/admin/products/price-exception/{productId}", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminProductPriceException(w, r, r.PathValue("productId"))
	}))

	mux.HandleFunc("/api/v1/admin/settings/margins", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminMargins(w, r, pricingRepo)
	}))

	mux.HandleFunc("/api/v1/admin/platform-pct", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminPlatformPct(w, r, pricingRepo)
	}))

	mux.HandleFunc("/api/v1/admin/settings/fee-overrides", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminFeeOverridesHistory(w, r, pricingRepo)
	}))

	mux.HandleFunc("/api/v1/admin/settings/product-fee-override", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminFeeOverridesHistory(w, r, pricingRepo)
	}))

	mux.HandleFunc("/api/v1/admin/margin-locks", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleAdminMarginLocks(w, r, pricingRepo)
	}))

	mux.HandleFunc("/api/v1/seller/variant-overrides", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) {
		handleSellerVariantOverrides(w, r, pricingRepo, storeRepo)
	}))

	// P2(a)/P2(c): seller per-store variant visibility + default variant.
	// The literal segments ("visibility", "default-variant") sit AFTER
	// {variantId}/{productId}, never a {param} directly after a literal
	// prefix that already has a sibling literal route (Nov 5 route
	// gate). Manual sub-path dispatch inside one mux pattern, like
	// the /api/v1/stores/ block.
	mux.HandleFunc("/api/v1/seller/stores/", RequireRoles(jwtService, "seller", "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/seller/stores/")
		parts := strings.Split(strings.Trim(sub, "/"), "/")
		// {slug}/products/{productId}/variants/{variantId}/visibility
		if len(parts) == 6 && parts[1] == "products" && parts[3] == "variants" && parts[5] == "visibility" {
			handleSellerStoreVariantVisibility(w, r, storeRepo, catalogRepo, parts[0], parts[2], parts[4])
			return
		}
		// {slug}/products/{productId}/default-variant
		if len(parts) == 4 && parts[1] == "products" && parts[3] == "default-variant" {
			handleSellerStoreVariantDefault(w, r, storeRepo, catalogRepo, parts[0], parts[2])
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/seller/pricing-settings", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) {
		handleSellerPricingSettings(w, r, pricingRepo)
	}))

	mux.HandleFunc("/api/v1/admin/reports/suspicious-transactions", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleGetSuspiciousTransactions(w, r, r.Context(), adminRepo)
	}))

	// 12. Supplier Lifecycle Endpoints & Admin Product Approvals
	mux.HandleFunc("/api/v1/admin/products/approve/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleApproveProposal(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/team", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method {
		case http.MethodGet:
			handleSupplierMembersList(w, r, supplierRepo)
		case http.MethodPost:
			handleSupplierMemberCreate(w, r, supplierRepo)
		default:
			writeMethodNotAllowed(w)
		}
	}))
	mux.HandleFunc("/api/v1/supplier/team/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			writeMethodNotAllowed(w)
			return
		}
		handleSupplierMemberStatus(w, r, supplierRepo)
	}))
	mux.HandleFunc("/api/v1/supplier/profile", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			supplierHandlers.HandleGetProfile(w, r)
		case http.MethodPut:
			supplierHandlers.HandleUpdateSupplierProfile(w, r)
		default:
			writeMethodNotAllowed(w)
		}
	}))

	// Supplier notification inbox (proposal_rejected outbox rows).
	// Same envelope as the customer /api/v1/notifications routes,
	// scoped to the supplier owner via RequireSupplierAccess.
	mux.HandleFunc("/api/v1/supplier/notifications", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		handleSupplierNotifications(w, r, db.DB)
	}))
	mux.HandleFunc("/api/v1/supplier/notifications/read", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		handleSupplierNotificationsRead(w, r, db.DB)
	}))

	// Gap 13: the legacy PUT /supplier/profile/shipping-settings
	// route is REMOVED with its handler — supports_direct_shipping
	// is no longer writable; the DTO field is derived from the
	// supplier_shipping_methods toggles.

	// Supplier Direct Dropshipping Routes
	mux.HandleFunc("/api/v1/supplier/shipments/pending", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleGetPendingShipments(w, r, r.Context(), shipmentRepo, supplierRepo)
	}))

	mux.HandleFunc("/api/v1/supplier/shipments/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/shipments/")
		if strings.HasSuffix(sub, "/tapin-label") && r.Method == http.MethodGet {
			shipmentID := strings.TrimSuffix(sub, "/tapin-label")
			shipmentID = strings.Trim(shipmentID, "/")
			handleSupplierTapinLabel(w, r, r.Context(), shipmentID, supplierRepo, tapinRepo)
			return
		}
		if strings.HasSuffix(sub, "/ship") && r.Method == http.MethodPost {
			shipmentID := strings.TrimSuffix(sub, "/ship")
			shipmentID = strings.Trim(shipmentID, "/")
			handleMarkShipmentShipped(w, r, r.Context(), shipmentID, shipmentRepo, orderRepo)
			return
		}
		if r.Method == http.MethodGet {
			handleGetSupplierShipments(w, r, r.Context(), shipmentRepo, supplierRepo)
			return
		}
		writeMethodNotAllowed(w)
	}))

	// FIX 46: exact /supplier/shipments serves live's carton-shipment
	// flow (ln): GET lists carton shipments, POST creates one. Order
	// shipments keep their /pending and subtree routes.
	mux.HandleFunc("/api/v1/supplier/shipments", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			supplierHandlers.HandleGetCartonShipments(w, r)
			return
		}
		if r.Method == http.MethodPost {
			supplierHandlers.HandleCreateCartonShipment(w, r)
			return
		}
		writeMethodNotAllowed(w)
	}))
	// Supplier Orders Endpoints
	mux.HandleFunc("/api/v1/supplier/orders", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleGetSupplierOrders(w, r, r.Context(), shipmentRepo, supplierRepo, orderRepo)
	}))

	mux.HandleFunc("/api/v1/supplier/warehouse-orders", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		handleGetSupplierWarehouseOrders(w, r, r.Context(), shipmentRepo, supplierRepo)
	}))

	mux.HandleFunc("/api/v1/supplier/orders/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/orders/")
		if (strings.HasSuffix(sub, "/shipping") || strings.HasSuffix(sub, "/ship")) && r.Method == http.MethodPost {
			orderID := strings.TrimSuffix(sub, "/shipping")
			orderID = strings.TrimSuffix(orderID, "/ship")
			orderID = strings.Trim(orderID, "/")
			handleSupplierSubmitShipping(w, r, r.Context(), orderID, shipmentRepo, orderRepo, supplierRepo, tapinRepo)
			return
		}
		if strings.HasSuffix(sub, "/accept") && r.Method == http.MethodPost {
			orderID := strings.TrimSuffix(sub, "/accept")
			orderID = strings.Trim(orderID, "/")
			handleSupplierAcceptOrder(w, r, r.Context(), orderID, shipmentRepo)
			return
		}
		if strings.HasSuffix(sub, "/addons") && r.Method == http.MethodGet {
			orderID := strings.TrimSuffix(sub, "/addons")
			orderID = strings.Trim(orderID, "/")
			supplierHandlers.HandleGetSupplierOrderAddons(w, r, orderID)
			return
		}
		if r.Method == http.MethodGet {
			handleGetSupplierOrders(w, r, r.Context(), shipmentRepo, supplierRepo, orderRepo)
			return
		}
		writeMethodNotAllowed(w)
	}))

	// Webhook for Carrier Tracking Updates (Authenticated via X-Webhook-Secret)
	mux.HandleFunc("/api/v1/webhooks/tracking/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		handleWebhookTrackingUpdate(w, r, r.Context(), shipmentRepo, orderRepo, cfg.TrackingWebhookSecret)
	})

	mux.HandleFunc("/api/v1/supplier/products/propose", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleProposeProduct(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/products/proposals", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleGetProposals(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/products", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleGetProducts(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/returns", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			supplierHandlers.HandleGetReturns(w, r)
			return
		}
		if r.Method == http.MethodPost {
			supplierHandlers.HandleCreateReturn(w, r)
			return
		}
		writeMethodNotAllowed(w)
	}))
	mux.HandleFunc("/api/v1/supplier/returns/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/cancel") && r.Method == http.MethodPost {
			supplierHandlers.HandleCancelReturn(w, r)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/inventory", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			handleGetSupplierInventory(w, r, r.Context(), supplierRepo)
			return
		}
		writeMethodNotAllowed(w)
	}))
	mux.HandleFunc("/api/v1/supplier/warranties", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			supplierHandlers.HandleGetWarranties(w, r)
			return
		}
		if r.Method == http.MethodPost {
			supplierHandlers.HandleRequestWarranty(w, r)
			return
		}
		writeMethodNotAllowed(w)
	}))

	// G4 (Batch 4b): bulk origin assign. Exact pattern, more specific than
	// the "/api/v1/supplier/products/" subtree below, so the mux routes it
	// here and never into the suffix dispatcher (which would 404 "origin/bulk").
	mux.HandleFunc("/api/v1/supplier/products/origin/bulk", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleBulkAssignProductOrigin(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/products/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/products/")
		if strings.HasSuffix(sub, "/supply-option") && r.Method == http.MethodPut {
			supplierHandlers.HandleUpdateSupplyOption(w, r)
			return
		}
		if strings.HasSuffix(sub, "/stock") && r.Method == http.MethodPut {
			supplierHandlers.HandleUpdateStock(w, r)
			return
		}
		// Gap 10: variant pricing must be matched BEFORE the product
		// pricing suffix below — {id}/variants/{vid}/pricing also ends in
		// "/pricing" and HandleUpdatePricing would otherwise swallow it,
		// treating the variant id as a product id.
		if strings.HasSuffix(sub, "/pricing") && r.Method == http.MethodPut {
			rest := strings.TrimSuffix(sub, "/pricing")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 3 && parts[1] == "variants" {
				supplierHandlers.HandleUpdateVariantPricing(w, r, parts[0], parts[2])
				return
			}
		}
		if strings.HasSuffix(sub, "/pricing") && r.Method == http.MethodPut {
			supplierHandlers.HandleUpdatePricing(w, r)
			return
		}
		if strings.HasSuffix(sub, "/variants") && r.Method == http.MethodGet {
			productID := strings.TrimSuffix(sub, "/variants")
			productID = strings.Trim(productID, "/")
			supplierHandlers.HandleGetProductVariants(w, r, productID)
			return
		}
		if strings.HasSuffix(sub, "/variants") && r.Method == http.MethodPost {
			productID := strings.TrimSuffix(sub, "/variants")
			productID = strings.Trim(productID, "/")
			supplierHandlers.HandleCreateProductVariant(w, r, productID)
			return
		}
		if strings.HasSuffix(sub, "/variants/bulk") && r.Method == http.MethodPost {
			productID := strings.TrimSuffix(sub, "/variants/bulk")
			productID = strings.Trim(productID, "/")
			supplierHandlers.HandleCreateProductVariantsBulk(w, r, productID)
			return
		}
		// P0 variant images/physical: variant routes must be matched
		// BEFORE the product "/images" branch below — a variant-images
		// path also ends in "/images" and HandleGetProductImages would
		// otherwise swallow it, treating "{id}/variants/{vid}" as a
		// product id (same class of bug as the "/pricing" ordering
		// comment above).
		if strings.HasSuffix(sub, "/images/reorder") && r.Method == http.MethodPut {
			rest := strings.TrimSuffix(sub, "/images/reorder")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 3 && parts[1] == "variants" {
				supplierHandlers.HandleReorderVariantImages(w, r, parts[0], parts[2])
				return
			}
		}
		if strings.HasSuffix(sub, "/images") && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
			rest := strings.TrimSuffix(sub, "/images")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 3 && parts[1] == "variants" {
				if r.Method == http.MethodGet {
					supplierHandlers.HandleGetVariantImages(w, r, parts[0], parts[2])
				} else {
					supplierHandlers.HandleUploadVariantImages(w, r, parts[0], parts[2], cfg.UploadDir)
				}
				return
			}
		}
		if strings.Contains(sub, "/images/") && r.Method == http.MethodDelete {
			parts := strings.Split(strings.Trim(sub, "/"), "/")
			if len(parts) == 5 && parts[1] == "variants" && parts[3] == "images" {
				supplierHandlers.HandleDeleteVariantImage(w, r, parts[0], parts[2], parts[4])
				return
			}
		}
		if strings.HasSuffix(sub, "/physical") && r.Method == http.MethodPut {
			rest := strings.TrimSuffix(sub, "/physical")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 3 && parts[1] == "variants" {
				supplierHandlers.HandleUpdateVariantPhysical(w, r, parts[0], parts[2])
				return
			}
		}
		// P1(c) variant sale: "{id}/variants/{vid}/sale" ends in
		// "/sale", which no product-level route uses, so no ordering
		// trap — but it is still matched on the full 3-part shape.
		if strings.HasSuffix(sub, "/sale") && r.Method == http.MethodPut {
			rest := strings.TrimSuffix(sub, "/sale")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 3 && parts[1] == "variants" {
				supplierHandlers.HandleUpdateVariantSale(w, r, parts[0], parts[2])
				return
			}
		}
		if strings.HasSuffix(sub, "/cart-limits") && r.Method == http.MethodPut {
			rest := strings.TrimSuffix(sub, "/cart-limits")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 3 && parts[1] == "variants" {
				supplierHandlers.HandleUpdateVariantCartLimits(w, r, parts[0], parts[2])
				return
			}
		}
		if strings.HasSuffix(sub, "/labels") && r.Method == http.MethodGet {
			productID := strings.TrimSuffix(sub, "/labels")
			productID = strings.Trim(productID, "/")
			supplierHandlers.HandleGetProductLabels(w, r, productID)
			return
		}
		if strings.HasSuffix(sub, "/images/reorder") && r.Method == http.MethodPut {
			productID := strings.TrimSuffix(sub, "/images/reorder")
			productID = strings.Trim(productID, "/")
			supplierHandlers.HandleReorderProductImages(w, r, productID)
			return
		}
		if strings.HasSuffix(sub, "/images") && (r.Method == http.MethodGet || r.Method == http.MethodPost) {
			productID := strings.TrimSuffix(sub, "/images")
			productID = strings.Trim(productID, "/")
			if r.Method == http.MethodGet {
				supplierHandlers.HandleGetProductImages(w, r, productID)
			} else {
				supplierHandlers.HandleUploadProductImages(w, r, productID, cfg.UploadDir)
			}
			return
		}
		if strings.HasSuffix(sub, "/cover") && r.Method == http.MethodPut {
			rest := strings.TrimSuffix(sub, "/cover")
			parts := strings.Split(strings.Trim(rest, "/"), "/")
			if len(parts) == 3 && parts[1] == "images" {
				supplierHandlers.HandleSetProductCoverImage(w, r, parts[0], parts[2])
				return
			}
		}
		if strings.Contains(sub, "/images/") && r.Method == http.MethodDelete {
			parts := strings.Split(strings.Trim(sub, "/"), "/")
			if len(parts) == 3 && parts[1] == "images" {
				supplierHandlers.HandleDeleteProductImage(w, r, parts[0], parts[2])
				return
			}
		}
		if strings.HasSuffix(sub, "/origin") && r.Method == http.MethodPut {
			productID := strings.TrimSuffix(sub, "/origin")
			productID = strings.Trim(productID, "/")
			supplierHandlers.HandleUpdateProductOrigin(w, r, productID)
			return
		}
		if sub == "" {
			supplierHandlers.HandleGetProducts(w, r)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/inbounds/create", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleCreateInbound(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/inbounds", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleGetInbounds(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/inbounds/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/inbounds/")
		if sub == "" {
			supplierHandlers.HandleGetInbounds(w, r)
			return
		}
		if sub == "create" && r.Method == http.MethodPost {
			supplierHandlers.HandleCreateInbound(w, r)
			return
		}
		if strings.HasSuffix(sub, "/dispatch") && r.Method == http.MethodPost {
			inboundID := strings.TrimSuffix(sub, "/dispatch")
			inboundID = strings.Trim(inboundID, "/")
			supplierHandlers.HandleDispatchInbound(w, r, inboundID)
			return
		}
		if strings.HasSuffix(sub, "/cancel") && r.Method == http.MethodPost {
			inboundID := strings.TrimSuffix(sub, "/cancel")
			inboundID = strings.Trim(inboundID, "/")
			supplierHandlers.HandleCancelInbound(w, r, inboundID)
			return
		}
		supplierHandlers.HandleGetInboundByID(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/campaigns", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetCampaigns(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/settlement/balance", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleSettlementBalance(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/campaigns/create", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleCreateCampaign(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/campaigns/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/campaigns/")
		if strings.HasSuffix(sub, "/status") && r.Method == http.MethodPut {
			supplierHandlers.HandleCampaignStatus(w, r)
			return
		}
		if strings.HasSuffix(sub, "/stats") && r.Method == http.MethodGet {
			supplierHandlers.HandleGetCampaignStats(w, r)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/sales-report", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleGetSalesReport(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/discrepancies", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleGetDiscrepancies(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/shipping-sla", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) { handleSupplierSLA(w, r, db.DB, supplierRepo) }))
	mux.HandleFunc("/api/v1/warehouse/picking-route", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) { handlePickingRoute(w, r, warehouseRepo) }))

	mux.HandleFunc("/api/v1/supplier/discrepancies/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/discrepancies/")
		if strings.HasSuffix(sub, "/respond") && r.Method == http.MethodPost {
			supplierHandlers.HandleRespondDiscrepancy(w, r)
			return
		}
		http.NotFound(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/settlement/request", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleRequestSettlement(w, r)
	}))

	mux.HandleFunc("/api/v1/supplier/settlement/history", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleGetSettlementHistory(w, r)
	}))

	// Team 06 live-parity supplier routes (D11/D12/D15). Exact line range
	// of this block is reported to the supervisor for integration, since
	// main.go is shared with other teams.
	mux.HandleFunc("/api/v1/supplier/contract/accept", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleSupplierContractAccept(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/contract/reconfirm", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleSupplierContractReconfirm(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/tapin/settings", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			supplierHandlers.HandleGetTapinSettings(w, r)
			return
		}
		if r.Method == http.MethodDelete {
			supplierHandlers.HandleDeleteTapinSettings(w, r)
			return
		}
		supplierHandlers.HandleUpdateTapinSettings(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/tapin/boxes", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleTapinBoxes(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/tapin/test", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleTapinTest(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/shipping-methods", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierShippingMethods(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/shipping-methods/request", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleRequestSupplierShippingMethod(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/pricing-settings", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			supplierHandlers.HandleGetSupplierPricingSettings(w, r)
			return
		}
		supplierHandlers.HandleUpdateSupplierPricingSettings(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/warehouses", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			supplierHandlers.HandleGetSupplierWarehouses(w, r)
			return
		}
		if r.Method == http.MethodPost {
			supplierHandlers.HandleCreateSupplierWarehouse(w, r)
			return
		}
		writeMethodNotAllowed(w)
	}))
	mux.HandleFunc("/api/v1/supplier/warehouses/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/warehouses/")
		if r.Method != http.MethodPut || sub == "" || sub == "central" || strings.Contains(sub, "/") {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleUpdateSupplierWarehouse(w, r, sub)
	}))
	mux.HandleFunc("/api/v1/supplier/warehouses/central", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetCentralWarehouses(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/exclusivity-challenges", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		// FIX 52 — POST creates a challenge (Jl challenge box); GET lists.
		if r.Method == http.MethodPost {
			supplierHandlers.HandleCreateSupplierExclusivityChallenge(w, r)
			return
		}
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierExclusivityChallenges(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/price-increases", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierPriceIncreases(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/price-increases/exception-ticket", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleSupplierPriceIncreaseExceptionTicket(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/stock-share", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierStockShare(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/customer-returns", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierCustomerReturns(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/return-shipping-charges", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierReturnShippingCharges(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/customer-returns/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/customer-returns/")
		if strings.HasSuffix(sub, "/request-back") && r.Method == http.MethodPost {
			returnItemID := strings.TrimSuffix(sub, "/request-back")
			returnItemID = strings.Trim(returnItemID, "/")
			supplierHandlers.HandleRequestSupplierReturnBack(w, r, returnItemID)
			return
		}
		if strings.HasSuffix(sub, "/cancel") && r.Method == http.MethodPost {
			parts := strings.Split(strings.Trim(sub, "/"), "/")
			if len(parts) == 3 && parts[0] == "back-requests" {
				supplierHandlers.HandleCancelSupplierReturnBack(w, r, parts[1])
				return
			}
		}
		if strings.HasSuffix(sub, "/complete") && r.Method == http.MethodPost {
			parts := strings.Split(strings.Trim(sub, "/"), "/")
			if len(parts) == 3 && parts[0] == "back-requests" {
				supplierHandlers.HandleCompleteSupplierReturnBack(w, r, parts[1])
				return
			}
		}
		writeMethodNotAllowed(w)
	}))

	mux.HandleFunc("/api/v1/supplier/settlement/storage-fees", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierStorageFees(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/analytics", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierAnalytics(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/addons", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			supplierHandlers.HandleGetSupplierAddons(w, r)
		case http.MethodPost:
			supplierHandlers.HandleCreateSupplierAddon(w, r)
		default:
			writeMethodNotAllowed(w)
		}
	}))
	mux.HandleFunc("/api/v1/supplier/addons/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/addons/")
		if strings.HasSuffix(sub, "/active") && r.Method == http.MethodPatch {
			supplierHandlers.HandleSetSupplierAddonActive(w, r, strings.TrimSuffix(sub, "/active"))
			return
		}
		if strings.HasSuffix(sub, "/attachments") && r.Method == http.MethodPut {
			supplierHandlers.HandleSetSupplierAddonAttachments(w, r, strings.TrimSuffix(sub, "/attachments"))
			return
		}
		if r.Method == http.MethodPut && sub != "" && !strings.Contains(sub, "/") {
			supplierHandlers.HandleUpdateSupplierAddon(w, r, sub)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/catalog/review/", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/supplier/catalog/review/")
		if strings.HasSuffix(sub, "/resubmit") && r.Method == http.MethodPost {
			proposalID := strings.TrimSuffix(sub, "/resubmit")
			proposalID = strings.Trim(proposalID, "/")
			supplierHandlers.HandleResubmitCatalogReview(w, r, proposalID)
			return
		}
		if r.Method == http.MethodGet {
			proposalID := strings.Trim(sub, "/")
			if proposalID != "" && !strings.Contains(proposalID, "/") {
				supplierHandlers.HandleGetCatalogReviewDetail(w, r, proposalID)
				return
			}
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/catalog/review", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleSupplierCatalogReview(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/products/implicit-agreements", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleGetSupplierImplicitAgreements(w, r)
	}))
	mux.HandleFunc("/api/v1/supplier/warranties/requests", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			supplierHandlers.HandleGetWarranties(w, r)
			return
		}
		if r.Method == http.MethodPost {
			supplierHandlers.HandleRequestWarranty(w, r)
			return
		}
		writeMethodNotAllowed(w)
	}))
	mux.HandleFunc("/api/v1/supplier/search-insight", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w)
			return
		}
		supplierHandlers.HandleSupplierSearchInsight(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/suppliers/list", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminSuppliersList(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/suppliers/records", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminSuppliersRecords(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/suppliers/pending", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminSuppliersPending(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/suppliers/shipping-methods", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminSuppliersShippingMethods(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/suppliers/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/suppliers/")
		if strings.HasSuffix(sub, "/approve") && r.Method == http.MethodPost {
			supplierHandlers.HandleAdminSupplierApprove(w, r, strings.TrimSuffix(sub, "/approve"))
			return
		}
		if strings.HasSuffix(sub, "/reject") && r.Method == http.MethodPost {
			supplierHandlers.HandleAdminSupplierReject(w, r, strings.TrimSuffix(sub, "/reject"))
			return
		}
		if strings.HasSuffix(sub, "/suspend") && r.Method == http.MethodPost {
			supplierHandlers.HandleAdminSupplierSuspend(w, r, strings.TrimSuffix(sub, "/suspend"))
			return
		}
		if strings.HasSuffix(sub, "/reactivate") && r.Method == http.MethodPost {
			supplierHandlers.HandleAdminSupplierReactivate(w, r, strings.TrimSuffix(sub, "/reactivate"))
			return
		}
		// Item 6 (admin supplier detail): GET /api/v1/admin/suppliers/<id>.
		// Bare single segment only (no "/" in sub), so it is disjoint
		// by shape from the four POST .../<id>/<action> branches above
		// and must stay AFTER them. In-closure branch, not a new mux
		// pattern: TestRouteTableNoConflicts cannot see it and it
		// cannot panic the ServeMux at boot; the ordering invariant
		// is asserted in TestManualDispatchOrderingInvariants.
		if r.Method == http.MethodGet && sub != "" && !strings.Contains(sub, "/") {
			supplierHandlers.HandleAdminSupplierDetail(w, r, sub)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/store-intel/suppliers", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		supplierHandlers.HandleAdminStoreIntelSuppliers(w, r)
	}))

	// ------------------------------------------------------------------
	// Team 01 (Phase 2, live parity): D01-D04, D06-D09, D13-D18 + manual
	// settlement. Route inventory (method, path, handler) is in the
	// Team 01 final report. main.go conflicts at integration are the
	// supervisor's to resolve; this block is self-contained.
	// Public content (D01), plans (D02), constants (D03), templates (D04).
	mux.HandleFunc("/api/v1/banners", func(w http.ResponseWriter, r *http.Request) { handleT01Banners(w, r, db.DB) })
	mux.HandleFunc("/api/v1/homepage-sections", func(w http.ResponseWriter, r *http.Request) { handleT01HomepageSections(w, r, db.DB) })
	mux.HandleFunc("/api/v1/site-content", func(w http.ResponseWriter, r *http.Request) { handleT01SiteContent(w, r, db.DB) })
	mux.HandleFunc("/api/v1/static-pages", func(w http.ResponseWriter, r *http.Request) { handleT01StaticPages(w, r, db.DB) })
	mux.HandleFunc("/api/v1/marketing-settings", func(w http.ResponseWriter, r *http.Request) { handleT01MarketingSettings(w, r, db.DB) })
	mux.HandleFunc("/api/v1/subscription-plans", func(w http.ResponseWriter, r *http.Request) { handleT01PublicPlans(w, r, db.DB) })
	mux.HandleFunc("/api/v1/public/return-window-days", func(w http.ResponseWriter, r *http.Request) { handleT01ReturnWindowDays(w, r, db.DB) })
	mux.HandleFunc("/api/v1/platform/contracts", func(w http.ResponseWriter, r *http.Request) { handleT01PlatformContracts(w, r, db.DB) })
	mux.HandleFunc("/api/v1/store-templates", func(w http.ResponseWriter, r *http.Request) { handleT01StoreTemplates(w, r, db.DB) })
	mux.HandleFunc("/api/v1/reviews", func(w http.ResponseWriter, r *http.Request) { handleT01Reviews(w, r, db.DB) })
	mux.HandleFunc("/api/v1/views/events", func(w http.ResponseWriter, r *http.Request) { handleT01ViewEvent(w, r, db.DB) })
	mux.HandleFunc("/api/v1/newsletter/subscribe", func(w http.ResponseWriter, r *http.Request) { handleT01NewsletterSubscribe(w, r, db.DB) })
	mux.HandleFunc("/api/v1/newsletter/unsubscribe", func(w http.ResponseWriter, r *http.Request) { handleT01NewsletterUnsubscribe(w, r, db.DB) })
	// Demo gateway (D18, demo-only, public: unauthenticated probe reaches validation).
	mux.HandleFunc("/api/v1/payments/demo-gateway/info", func(w http.ResponseWriter, r *http.Request) { handleT01DemoGatewayInfo(w, r, db.DB) })
	mux.HandleFunc("/api/v1/payments/demo-gateway/status", func(w http.ResponseWriter, r *http.Request) { handleT01DemoGatewayStatus(w, r, db.DB) })
	mux.HandleFunc("/api/v1/payments/demo-gateway/callback", func(w http.ResponseWriter, r *http.Request) { handleT01DemoGatewayCallback(w, r, db.DB) })
	// Geo (D06): probe 401, so any valid token is required; role is unverified.
	mux.HandleFunc("/api/v1/geo/provinces", AuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) { handleT01GeoProvinces(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/geo/cities", AuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) { handleT01GeoCities(w, r, db.DB) }))
	// Customer (D06, D07) and logout (D08).
	mux.HandleFunc("/api/v1/wishlist", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01Wishlist(w, r, db.DB, storeRepo) }))
	mux.HandleFunc("/api/v1/addresses", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01Addresses(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/addresses/", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/addresses/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AddressByID(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/customer/addresses", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01Addresses(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/customer/addresses/", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/customer/addresses/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AddressByID(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/customer/reviews", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01CustomerReviews(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/customer/wallet", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01CustomerWallet(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/customer/wallet/quote", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01CustomerWalletQuote(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/customer/wallet/withdrawals", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01CustomerWithdrawals(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/notifications", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01Notifications(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/notifications/read", RequireRoles(jwtService, "customer")(func(w http.ResponseWriter, r *http.Request) { handleT01NotificationsRead(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/auth/logout", AuthMiddleware(jwtService, func(w http.ResponseWriter, r *http.Request) { handleT01Logout(w, r, db.DB) }))
	// Seller insight (D09) and store templates (D04).
	mux.HandleFunc("/api/v1/seller/analytics", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleT01SellerAnalytics(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/seller/search-insight", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleT01SellerSearchInsight(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/seller/store/template", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleT01SellerStoreTemplate(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/seller/store/template/publish", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleT01SellerStoreTemplatePublish(w, r, db.DB) }))
	// Seller notification inbox (Phase 2, W2): outbox rows addressed
	// to the calling store owner (order/finance notices from W1).
	// Same envelope + shared helpers as the supplier inbox.
	mux.HandleFunc("/api/v1/seller/notifications", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleSellerNotifications(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/seller/notifications/read", RequireRoles(jwtService, "seller")(func(w http.ResponseWriter, r *http.Request) { handleSellerNotificationsRead(w, r, db.DB) }))
	// Admin insight (D13).
	mux.HandleFunc("/api/v1/admin/subscription-plans", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminPlans(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/overview", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminOverview(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/overview/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/overview/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminOverviewSub(w, r, db.DB, sub)
	}))
	// Admin notification inbox (Phase 2, W2): outbox rows addressed
	// to the calling admin user (dispute-filed etc. from W1). Same
	// envelope + shared helpers as the supplier inbox.
	// Critic fix (Phase 2): the gate admits the finance queue too
	// ("finance" admits finance_specialist and vice versa, plus
	// support_specialist) — W1 addresses dispute-filed and
	// payout-requested rows to those users, so the inbox must let
	// them read their own rows. Sellers/warehouse staff still cannot
	// read this inbox; their own inboxes are separate routes.
	mux.HandleFunc("/api/v1/admin/notifications", RequireRoles(jwtService, "admin", "finance", "support_specialist")(func(w http.ResponseWriter, r *http.Request) { handleAdminNotifications(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/notifications/read", RequireRoles(jwtService, "admin", "finance", "support_specialist")(func(w http.ResponseWriter, r *http.Request) { handleAdminNotificationsRead(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/growth/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/growth/")
		// Fix69 R1: the live bundle also calls /admin/growth/ads/fraud,
		// so exactly that nested sub is allowed through the same admin
		// guard; every other nested sub still 404s. The fraud payload
		// itself lives in handleT01AdminGrowth (admin_insight_handlers.go).
		if sub == "" || (strings.Contains(sub, "/") && sub != "ads/fraud") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminGrowth(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/admin/support/tickets", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSupportTickets(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/support/metrics/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/support/metrics/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminSupportMetrics(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/admin/store-intel/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		// NOTE: admin/store-intel/suppliers is owned by Team 06 and is
		// registered as an exact pattern above; the mux prefers it.
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/store-intel/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminStoreIntel(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/admin/content/health", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminContentHealth(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/referral-network", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminReferralNetwork(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/ad-summary", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminAdSummary(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/ads/stats", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminAdsStats(w, r, db.DB) }))
	// Admin settings family, non-pricing parts (D14). Pricing/margins/
	// platform-pct/fee-history stay with Team 02; base-fee is NOT restored.
	mux.HandleFunc("/api/v1/admin/settings/platform", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsPlatform(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/shipping", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsShipping(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/marketing", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsMarketing(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/contracts", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsContracts(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/settlement", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsSettlement(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/campaign-budget", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsCampaignBudget(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/trial-quota", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsTrialQuota(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/warehouse-tariffs", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsWarehouseTariffs(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/packaging-tariffs", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsPackagingTariffs(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/tapin-tariff", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsTapinTariff(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/tapin-platform", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsTapinPlatform(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/tapin-boxes", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsTapinBoxes(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/supplier-contract", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsSupplierContract(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/settings/storage-fees", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSettingsStorageFees(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/storage-fees/run", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleAdminStorageFeeRun(w, r, supplierRepo) }))
	// Admin operations (D15). Supplier admin routes stay with Team 06.
	mux.HandleFunc("/api/v1/admin/banners", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminBanners(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/banners/sync-sections", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminBannersSyncSections(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/banners/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/banners/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminBannerByID(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/admin/homepage-sections", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminHomepageSections(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/homepage-sections/history", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminHomepageSectionsHistory(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/homepage-sections/reorder", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminHomepageSectionsReorder(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/homepage-sections/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/homepage-sections/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminHomepageSectionByID(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/admin/site-content", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSiteContent(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/static-pages", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminStaticPages(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/static-pages/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/static-pages/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminStaticPageByKey(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/admin/categories", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminCategories(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/warehouses", requireGlobalWarehouseRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminWarehouses(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/warranties", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminGenericList(w, r, db.DB, "warranty_property_requests", "warranties", "")
	}))
	mux.HandleFunc("/api/v1/admin/tickets", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSupportTickets(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/tickets/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/tickets/")
		if strings.HasSuffix(sub, "/reply") {
			handleT01AdminTicketReply(w, r, db.DB, strings.TrimSuffix(sub, "/reply"))
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/disputes", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminGenericList(w, r, db.DB, "disputes", "disputes", "")
	}))
	mux.HandleFunc("/api/v1/admin/returns", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminGenericList(w, r, db.DB, "customer_returns", "returns", "")
	}))
	mux.HandleFunc("/api/v1/admin/reviews", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminGenericList(w, r, db.DB, "product_reviews", "reviews", "")
	}))
	mux.HandleFunc("/api/v1/admin/reviews/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/reviews/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01AdminReviewByID(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/admin/payouts", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminPayouts(w, r, db.DB) }))
	// Fix69 (W-ROUTES) R4: POST /api/v1/admin/payouts/<id>/confirm-manual-transfer
	// (live: AdminDashboardView-MERGED.js confirmManualTransfer).
	mux.HandleFunc("/api/v1/admin/payouts/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { wr69AdminPayouts(w, r, db.DB, finRepo) }))
	mux.HandleFunc("/api/v1/admin/orders/pending-payment", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleAdminPendingPayments(w, r, db.DB)
	}))
	mux.HandleFunc("/api/v1/admin/payments", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminGenericList(w, r, db.DB, "payment_transactions", "payments", "")
	}))
	// Fix69 (W-ROUTES) R4: POST /api/v1/admin/payments/<id>/refund
	// (live: AdminDashboardView-MERGED.js refundPayment).
	mux.HandleFunc("/api/v1/admin/payments/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { wr69AdminPayments(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/customer-withdrawals", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminCustomerWithdrawals(w, r, db.DB)
	}))
	mux.HandleFunc("/api/v1/admin/customer-withdrawals/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/customer-withdrawals/")
		if strings.HasSuffix(sub, "/review") {
			handleT01AdminCustomerWithdrawalReview(w, r, db.DB, strings.TrimSuffix(sub, "/review"))
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/newsletter/subscribers", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminNewsletterSubscribers(w, r, db.DB) }))
	// D1 (Batch 4a): read-only list of failed PSP callback retries.
	mux.HandleFunc("/api/v1/admin/callback-retries", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminCallbackRetries(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/addons", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminGenericList(w, r, db.DB, "product_addons", "addons", "")
	}))
	// Team 07 (D15): GET /api/v1/admin/staff replaces the Team 01 501 stub
	// with a real list of staff-role users (see team07_identity_handlers.go).
	// The exact live staff response shape is unverified (U54).
	mux.HandleFunc("/api/v1/admin/staff", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT07AdminStaff(w, r, db.DB)
	}))
	mux.HandleFunc("/api/v1/admin/staff/", RequireCapabilities(jwtService, "super_admin")(func(w http.ResponseWriter, r *http.Request) {
		handleWarehouseStaffCity(w, r, db.DB)
	}))
	// Team 07 (D58): minimal super_admin-guarded admin capability
	// grant/revoke. The exact live endpoint is unverified (U55); these
	// routes are a documented new mechanism, not a claimed live route.
	mux.HandleFunc("/api/v1/admin/capabilities/grant", RequireCapabilities(jwtService, "super_admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT07AdminCapabilityGrant(w, r, db.DB, capabilityRepo)
	}))
	mux.HandleFunc("/api/v1/admin/capabilities/revoke", RequireCapabilities(jwtService, "super_admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT07AdminCapabilityRevoke(w, r, db.DB, capabilityRepo)
	}))
	// Jobs scheduler state (live Na.list / Na.runNow,
	// AdminDashboardView-MERGED.js bytes 60078/60150): GET
	// /admin/jobs -> {jobs: [...]}, POST /admin/jobs/{id}/run ->
	// {job: ...}. The generic job_runs dump this replaced
	// returned snake_case rows the jobs tab never read (its
	// error strip reads camelCase lastRunError).
	mux.HandleFunc("/api/v1/admin/jobs", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminJobs(w, r, db.DB)
	}))
	mux.HandleFunc("/api/v1/admin/jobs/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/jobs/")
		if strings.HasSuffix(suffix, "/run") {
			id := strings.TrimSuffix(suffix, "/run")
			if id != "" && !strings.Contains(id, "/") {
				var run func(context.Context) error
				if backgroundWorker != nil {
					run = backgroundWorker.RunDeliveryJob
				}
				handleT01AdminJobRun(w, r, db.DB, id, run)
				return
			}
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/admin/supplier-promotions", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminSupplierPromotions(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/products/missing-measurements", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminProductsMissingMeasurements(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/products/implicit-agreements", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { handleT01AdminProductsImplicitAgreements(w, r, db.DB) }))
	// Fix69 (W-ROUTES) R2: GET /api/v1/admin/exclusivity-challenges[?status=]
	// and POST /api/v1/admin/exclusivity-challenges/<id>/review
	// (live: index-MERGED.js getExclusivityChallenges /
	// reviewExclusivityChallenge).
	mux.HandleFunc("/api/v1/admin/exclusivity-challenges", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { wr69AdminExclusivityChallenges(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/exclusivity-challenges/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { wr69AdminExclusivityChallenges(w, r, db.DB) }))
	// Fix69 (W-ROUTES) R3: admin-guarded /api/v1/admin/products/
	// subtree: PATCH <id>/brand, PATCH <id>/originality,
	// GET/PUT <id>/pricing (live: index-MERGED.js setProductBrand /
	// setProductOriginality, AdminDashboardView-MERGED.js pricing).
	// More-specific existing registrations above keep mux precedence;
	// the bare /api/v1/admin/products path 301-redirects here.
	mux.HandleFunc("/api/v1/admin/products/", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) { wr69AdminProducts(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/admin/catalog/review", RequireRoles(jwtService, "admin")(func(w http.ResponseWriter, r *http.Request) {
		handleT01AdminGenericList(w, r, db.DB, "proposal_field_reviews", "reviews", "")
	}))
	// Finance (D16) and manual settlement (live only).
	mux.HandleFunc("/api/v1/finance/treasury/", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/finance/treasury/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01FinanceTreasury(w, r, db.DB, sub)
	}))
	// Fix69 (W-ROUTES) R5: POST /api/v1/finance/adjustments added;
	// GET still delegates to the existing 501 stub inside
	// wr69FinanceAdjustments (finance_parity_handlers.go untouched).
	mux.HandleFunc("/api/v1/finance/adjustments", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { wr69FinanceAdjustments(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/finance/reports/revenue/monthly", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { handleT01FinanceRevenueMonthly(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/finance/settlements/monthly", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { handleT01MonthlyList(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/finance/settlements/monthly/run", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { handleT01MonthlyRun(w, r, db.DB) }))
	// FIN-013 § 2.4: unknown-outcome refund work queue (list) + manual
	// reconcile trigger. Both are finance-role only; the trigger is a
	// 503 until FIN-010 supplies a PSP refund adapter.
	mux.HandleFunc("/api/v1/finance/refunds/unknown-queue", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { handleFIN013RefundUnknownQueue(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/finance/refunds/reconcile", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { handleFIN013RefundReconcile(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/finance/settlements/monthly/", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/finance/settlements/monthly/")
		if sub == "" || strings.Contains(sub, "/") {
			http.NotFound(w, r)
			return
		}
		handleT01MonthlyDetail(w, r, db.DB, sub)
	}))
	mux.HandleFunc("/api/v1/finance/settlements/approved", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { handleT01SettlementsApproved(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/finance/early-withdrawals", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) { handleT01EarlyWithdrawalsList(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/finance/early-withdrawals/", RequireRoles(jwtService, financeRoles...)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/finance/early-withdrawals/")
		if strings.HasSuffix(sub, "/review") {
			handleT01EarlyWithdrawalReview(w, r, db.DB, strings.TrimSuffix(sub, "/review"))
			return
		}
		http.NotFound(w, r)
	}))
	// Warehouse (D17): registrations only, wired to existing queries.
	mux.HandleFunc("/api/v1/warehouse/me", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) { handleT01WarehouseMe(w, r, db.DB) }))
	mux.HandleFunc("/api/v1/warehouse/orders/ready-to-pack", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleT01WarehouseOrdersReady(w, r, r.Context(), warehouseRepo, "pack")
	}))
	mux.HandleFunc("/api/v1/warehouse/orders/ready-to-ship", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleT01WarehouseOrdersReady(w, r, r.Context(), warehouseRepo, "ship")
	}))
	// Slash pattern only: Go mux 301-redirects the bare path to it,
	// matching the live probe for /warehouse/shipments.
	mux.HandleFunc("/api/v1/warehouse/shipments/", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		sub := strings.TrimPrefix(r.URL.Path, "/api/v1/warehouse/shipments/")
		if sub == "" {
			handleT01WarehouseShipments(w, r, db.DB)
			return
		}
		if strings.HasSuffix(sub, "/receive") && r.Method == http.MethodPost {
			handleReceiveWarehouseShipment(w, r, r.Context(), strings.TrimSuffix(sub, "/receive"), supplierRepo)
			return
		}
		if r.Method == http.MethodGet {
			handleGetWarehouseShipment(w, r, r.Context(), sub, supplierRepo)
			return
		}
		http.NotFound(w, r)
	}))
	mux.HandleFunc("/api/v1/warehouse/return-backs", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) { handleT01WarehouseReturnBacks(w, r, db.DB) }))
	// Team 03 (D10): warehouses/returns from the customer returns engine.
	// Distinct from /warehouse/return-backs above; that stub is untouched.
	mux.HandleFunc("/api/v1/warehouses/returns", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) { handleWarehousesReturns(w, r, r.Context(), orderRepo) }))
	mux.HandleFunc("/api/v1/warehouse/putaway", requireWarehouseCityRoles(jwtService, warehouseRoles...)(func(w http.ResponseWriter, r *http.Request) {
		handleT01WarehousePutaway(w, r, r.Context(), warehouseRepo)
	}))
	// Static File Server & Upload Routes (Secured with role requirement supplier/admin & content inspection)
	mux.Handle("/static/", staticFileHandler())
	mux.HandleFunc("/api/v1/upload/product-image", RequireSupplierAccess(jwtService, supplierRepo)(func(w http.ResponseWriter, r *http.Request) {
		HandleProductImageUpload(w, r, cfg.UploadDir)
	}))

	if db != nil {
		observability.Default.SetDB(db.DB)
		// PERF-03 G5: sample pool waits every 100 ms into the
		// sellersho_db_wait_seconds histogram so the pool-wait-p95
		// gate is measurable.
		observability.Default.StartDBWaitSampling(db.DB, 100*time.Millisecond)
	}
	if err := startupCtx.Err(); err != nil {
		return errors.New("startup canceled or exceeded STARTUP_TIMEOUT_SECONDS")
	}
	addr := fmt.Sprintf(":%s", cfg.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return errors.New("HTTP listener bind failed; check PORT availability")
	}
	defer listener.Close()
	// OBS-01 deploy-fix (2026-11-18): the metrics listener binds all
	// interfaces INSIDE the container (0.0.0.0:9090). The old
	// 127.0.0.1 bind was unreachable at the container's eth0 IP, and
	// Docker publishing forwards exactly there, so the loopback-only
	// host publish (127.0.0.1:9090:9090) could never reach it. The
	// safety boundary is now the host publish, not the in-container
	// bind — see ValidateListenAddress in internal/observability and
	// docs/operations/observability.md.
	metricsServer, metricsErr := observability.Default.Start("0.0.0.0:9090")
	if metricsErr != nil {
		return errors.New("metrics listener bind failed; check local metrics port availability")
	}
	metricsCtx, stopMetrics := context.WithCancel(context.Background())
	metricsDone := make(chan struct{})
	go func() {
		defer close(metricsDone)
		observability.Default.PollDependencies(metricsCtx, redisClient.Client)
	}()
	process.StopMetrics = func(ctx context.Context) error {
		stopMetrics()
		observability.Default.StopDBWaitSampling()
		closeErr := metricsServer.Close()
		select {
		case <-metricsDone:
			return closeErr
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	log.Printf("==================================================")
	log.Printf("Drup Platform — Go Backend Core v1.0.0")
	log.Printf("Running at http://localhost%s", addr)
	log.Printf("==================================================")

	server := newHTTPServer(addr, observability.Default.HTTP(mux, state.Admission(enableCORS(requestbudget.NewWithReporter(requestbudget.DefaultConfig(), observability.Default)(mux), cfg))))

	process.Server = server
	stopStartup()
	return process.Serve(signalCtx, listener, backgroundWorker.Start)
}
