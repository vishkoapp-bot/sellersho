# Project: Sellersho Fashion Storefront Redesign

## Architecture
- **Frontend Stack**: React 19, Vite 6, Tailwind CSS v4 (`@tailwindcss/vite`), motion (`motion/react` v12), Lucide React (stroke width 1.5px), Vazirmatn Persian typography.
- **Storefront Architecture**: 
  - Dynamic template resolution via `TemplateRegistry.tsx` (`tpl-fashion-1`).
  - Native fashion palette & editorial layout: `#faf8f5` canvas, `#1c1c1a` ink, `#efeae3` announcement, `#e5e0d8` borders, `#3a4441` footer band, sharp 2px radius.
  - Storefront shell in `src/App.tsx`, `SellerStorefrontView.tsx`, `SellerStorefrontHeader.tsx`, `SellerStorefrontFooter.tsx`.
- **Backend / Deployment**:
  - Go HTTP server (`backend/cmd/server/store_template_handlers.go`).
  - Production server at ParsPack IP `46.249.98.190` in `/opt/drup` with Docker containers (`drup_frontend`, `drup_backend`, `drup_postgres`, `drup_redis`).
  - SSH key at `C:\Users\LENOVO\.ssh\sellersho_parspack.pem`.

## Feature Inventory
| # | Feature | Description | Milestone | Source |
|---|---------|-------------|-----------|--------|
| 1 | Editorial Hero & Banner Sections | Overhauled visual design for hero sections, editorial banners, lookbook and category displays | M1 | Survey 1, Survey 2 |
| 2 | Modern Fashion Product Card | Enhanced `AastinProductCard` with second-image hover flip (`product.images[1]`), smooth zoom, desktop quick view trigger, and minimalist badges | M1 | Survey 1, Survey 2 |
| 3 | Dedicated Fashion Header Variant | Add `variant="fashion"` to `SellerStorefrontHeader` with glassmorphic backdrop `#faf8f5/90`, minimal monochrome icons, eliminating teal circle clash | M1 | Survey 1, Survey 2 |
| 4 | Harmonized Fashion Footer | Seamless integration between FashionTemplate's `#3a4441` band and `SellerStorefrontFooter`, removing conflicting 32px rounded arch | M1 | Survey 1, Survey 2 |
| 5 | Full Responsive Layout Optimization | Eliminate `max-w-[504px]` tablet bottleneck in `src/App.tsx`; verify clean layouts and >=44px touch targets on 375px, 390px, 768px, and 1280px+ viewports without horizontal overflow | M1 | Survey 2, Requirements |
| 6 | Template Registry & State Continuity | Preserve all existing props, dynamic data bindings (`products`, `categories`, `store`), and registry loading without console errors | M1 | Survey 1, Acceptance Criteria |
| 7 | Production Build Verification | Complete `npm run build` with exit code 0 | M2 | Survey 3, Acceptance Criteria |
| 8 | Git Commit | Commit all changes cleanly to Git repository (`vishkoapp-bot/sellersho`) | M2 | Requirements |
| 9 | Production Server Deployment | Deploy updated application to ParsPack production server at `46.249.98.190` in `/opt/drup` | M2 | Requirements, Survey 3 |

## Milestones
| # | Name | Scope | Dependencies | Status |
|---|------|-------|-------------|--------|
| 1 | Fashion Storefront Redesign & Responsiveness | `FashionTemplate.tsx`, `SellerStorefrontHeader.tsx`, `SellerStorefrontFooter.tsx`, `src/App.tsx`, `TemplateRegistry.tsx` | none | IN_PROGRESS |
| 2 | Production Build, Git Commit & Server Deployment | `npm run build`, Git commit, deployment to `46.249.98.190` (`/opt/drup`) | M1 | PLANNED |

## Interface Contracts
### FashionTemplate ↔ Storefront Shell
- `FashionTemplate`: `React.FC<StorefrontTemplateProps>` receiving `store`, `products`, `accentColor`, `config`, `categories`.
- `SellerStorefrontHeader`: props include `variant: 'modern' | 'digikala' | 'fashion'`.
- `AastinProductCard`: supports `product: Product`, `onSelect: (p: Product) => void`, image fallback, secondary hover image if `product.images?.length > 1`.

## Code Layout
- `src/components/storefront/templates/FashionTemplate.tsx`: Core fashion template components, editorial blocks, `AastinProductCard`.
- `src/components/storefront/SellerStorefrontHeader.tsx`: Storefront navigation header.
- `src/components/storefront/SellerStorefrontFooter.tsx`: Storefront footer.
- `src/components/storefront/SellerStorefrontView.tsx`: Storefront view container passing header variant and layout props.
- `src/App.tsx`: App shell container and responsive width constraints.
