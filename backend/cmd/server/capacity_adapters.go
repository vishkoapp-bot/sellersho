package main

import (
	"errors"
	"regexp"

	"github.com/drup/platform/internal/payment"
)

// selectCapacityAdapters is a startup-only opt-in for the disposable harness.
// It never changes the ordinary real/mock selection. The runner creates the
// named carrier alias on its marked internal network; accepting arbitrary URLs,
// localhost alone, credentials, paths or query parameters would defeat that seam.
// Validate everything before applying either adapter, and never echo input.
func selectCapacityAdapters(getenv func(string) string, ordinary payment.PaymentGatewayClient) (payment.PaymentGatewayClient, string, error) {
	carrier := getenv("CAPACITY_CARRIER_BASE_URL")
	if carrier != "" {
		if getenv("APP_ENV") != "test" || !regexp.MustCompile(`^[a-f0-9]{24}$`).MatchString(getenv("SELLERSHO_CAPACITY_RUN")) {
			return nil, "", errors.New("capacity carrier requires test environment and private run marker")
		}
		if carrier != "http://carrier:8082" {
			return nil, "", errors.New("capacity carrier endpoint must be the isolated harness alias")
		}
	}
	client, enabled, err := payment.CapacityGatewayFromEnvironment(getenv)
	if err != nil {
		return nil, "", err
	}
	if !enabled {
		client = ordinary
	}
	return client, carrier, nil
}
