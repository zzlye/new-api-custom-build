package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/stretchr/testify/assert"
)

func TestPaymentReturnURLUsesSuppliedDefaultDashboardPath(t *testing.T) {
	previousAddress := system_setting.ServerAddress
	system_setting.ServerAddress = "https://dashboard.example.com/"
	t.Cleanup(func() { system_setting.ServerAddress = previousAddress })

	assert.Equal(t, "https://dashboard.example.com/wallet", PaymentReturnURL("/wallet"))
}

func TestPaymentCallbackAddressFollowsSettingsAndPreservesOverride(t *testing.T) {
	previousServer, previousCallback := system_setting.ServerAddress, operation_setting.CustomCallbackAddress
	t.Cleanup(func() {
		system_setting.ServerAddress, operation_setting.CustomCallbackAddress = previousServer, previousCallback
	})
	for _, tc := range []struct{ server, callback, want string }{
		{"https://new.example/root/", "", "https://new.example/root"},
		{"https://new.example/root/", "https://callback.example/prefix/", "https://callback.example/prefix"},
		{"https://changed.example/", "", "https://changed.example"},
	} {
		system_setting.ServerAddress, operation_setting.CustomCallbackAddress = tc.server, tc.callback
		assert.Equal(t, tc.want+"/api/user/epay/notify", GetCallbackAddress()+"/api/user/epay/notify")
	}
}
