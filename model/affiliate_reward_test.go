package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGrantTopUpInviteCommissionCreditsBalanceImmediately(t *testing.T) {
	truncateTables(t)

	originalRatio := common.InviteTopUpCommissionRatio
	common.InviteTopUpCommissionRatio = 0.15
	t.Cleanup(func() {
		common.InviteTopUpCommissionRatio = originalRatio
	})

	inviter := User{
		Username: "commission-inviter",
		Quota:    100,
		AffCode:  "COMMISSION-INVITER",
	}
	require.NoError(t, DB.Create(&inviter).Error)

	invitee := User{
		Username:  "commission-invitee",
		InviterId: inviter.Id,
		AffCode:   "COMMISSION-INVITEE",
	}
	require.NoError(t, DB.Create(&invitee).Error)

	result, err := GrantTopUpInviteCommission(invitee.Id, 1000)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, inviter.Id, result.InviterId)
	assert.Equal(t, 150, result.CommissionQuota)

	var got User
	require.NoError(t, DB.First(&got, inviter.Id).Error)
	assert.Equal(t, 250, got.Quota)
	assert.Zero(t, got.AffQuota)
	assert.Equal(t, 150, got.AffHistoryQuota)
}

func TestReconcileAffiliateDataMigratesLegacyRewardsOnce(t *testing.T) {
	truncateTables(t)

	inviter := User{
		Username:        "legacy-inviter",
		Quota:           100,
		AffQuota:        150,
		AffHistoryQuota: 150,
		AffCode:         "LEGACY-INVITER",
	}
	require.NoError(t, DB.Create(&inviter).Error)

	invitee := User{
		Username:  "legacy-invitee",
		InviterId: inviter.Id,
		AffCode:   "LEGACY-INVITEE",
	}
	require.NoError(t, DB.Create(&invitee).Error)

	require.NoError(t, reconcileAffiliateData(DB))
	require.NoError(t, reconcileAffiliateData(DB))

	var got User
	require.NoError(t, DB.First(&got, inviter.Id).Error)
	assert.Equal(t, 250, got.Quota)
	assert.Zero(t, got.AffQuota)
	assert.Equal(t, 150, got.AffHistoryQuota)
	assert.Equal(t, 1, got.AffCount)
}
