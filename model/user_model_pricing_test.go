package model

import (
	"maps"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestUserModelPricingDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN not configured")
				}
				driver = postgres.Open(dsn)
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: "userprice_test_"}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			oldDB, oldType := DB, common.MainDatabaseType()
			common.OptionMapRWMutex.Lock()
			oldOptions := maps.Clone(common.OptionMap)
			common.OptionMap = map[string]string{}
			common.OptionMapRWMutex.Unlock()
			DB = db
			common.SetMainDatabaseType(common.DatabaseType(dialect))
			initCol()
			t.Cleanup(func() {
				require.NoError(t, db.Migrator().DropTable(&Task{}, &Channel{}, &User{}, &Option{}))
				DB = oldDB
				common.SetMainDatabaseType(oldType)
				initCol()
				common.OptionMapRWMutex.Lock()
				common.OptionMap = oldOptions
				common.OptionMapRWMutex.Unlock()
				require.NoError(t, sqlDB.Close())
			})
			require.NoError(t, db.AutoMigrate(&User{}, &Option{}, &Channel{}, &Task{}))
			versionQuery := "SELECT version()"
			if dialect == "sqlite" {
				versionQuery = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, db.Raw(versionQuery).Scan(&version).Error)
			t.Logf("database version: %s", version)
			require.NoError(t, db.Create(&User{Id: 81, Username: "pricing-user", Password: "test-only"}).Error)
			task := Task{TaskID: "personal-pricing-task", UserId: 81, PrivateData: TaskPrivateData{BillingContext: &TaskBillingContext{UserPricing: true, ModelPrice: 0.08, GroupRatio: 0.4}}}
			require.NoError(t, db.Create(&task).Error)
			var loaded Task
			require.NoError(t, db.First(&loaded, task.ID).Error)
			require.NotNil(t, loaded.PrivateData.BillingContext)
			assert.True(t, loaded.PrivateData.BillingContext.UserPricing)
			assert.Equal(t, 0.08, loaded.PrivateData.BillingContext.ModelPrice)
			assert.Equal(t, 0.4, loaded.PrivateData.BillingContext.GroupRatio)
			// 原有全局价格和旧用户在重复启动后保持完整，无须新增表或改字段。
			require.NoError(t, db.Create(&Option{Key: "ModelPrice", Value: `{"image":0.1}`}).Error)
			for range 2 {
				require.NoError(t, db.AutoMigrate(&User{}, &Option{}, &Channel{}, &Task{}))
			}
			empty, err := GetUserModelPricingSnapshot(81)
			require.NoError(t, err)
			assert.Empty(t, empty.Entries)
			change := ModelPricingChange{ModelName: "image", ExpectedVersion: empty.EmptyVersion, Pricing: PricingValues{"ModelPrice": 0.08}}
			require.NoError(t, UpdateUserModelPricing(81, []ModelPricingChange{change}))
			snapshot, err := GetUserModelPricingSnapshot(81)
			require.NoError(t, err)
			require.Len(t, snapshot.Entries, 1)
			assert.Equal(t, 0.08, snapshot.Entries[0].Effective["ModelPrice"])
			assert.ErrorIs(t, UpdateUserModelPricing(81, []ModelPricingChange{change}), ErrModelPricingConflict)
			change.ExpectedVersion = snapshot.Entries[0].Version
			change.Pricing = PricingValues{"ModelPrice": -1.0}
			assert.Error(t, UpdateUserModelPricing(81, []ModelPricingChange{change}))
			assert.Error(t, UpdateUserModelPricing(999, []ModelPricingChange{change}))
			assert.Error(t, UpdateOption("UserModelPricing:81", "{}"))
			assert.Error(t, UpdateOptionsBulk(map[string]string{"UserModelPricing:81": "{}"}))
			global := []Pricing{{ModelName: "image", ModelPrice: 0.1}}
			personal, err := PricingForUser(global, 81)
			require.NoError(t, err)
			assert.Equal(t, 0.08, personal[0].ModelPrice)
			assert.Equal(t, 0.1, global[0].ModelPrice)
			other, err := PricingForUser(global, 82)
			require.NoError(t, err)
			assert.Equal(t, global, other)
			// 显式零价和重启回读不能变成未配置。
			change.Pricing = PricingValues{"ModelPrice": 0.0}
			require.NoError(t, UpdateUserModelPricing(81, []ModelPricingChange{change}))
			for range 2 {
				require.NoError(t, db.AutoMigrate(&User{}, &Option{}, &Channel{}, &Task{}))
				var row Option
				require.NoError(t, db.Where(commonKeyCol+" = ?", "UserModelPricing:81").First(&row).Error)
				require.NoError(t, updateOptionMap(row.Key, row.Value))
			}
			snapshot, err = GetUserModelPricingSnapshot(81)
			require.NoError(t, err)
			assert.Equal(t, 0.0, snapshot.Entries[0].Effective["ModelPrice"])
			change.ExpectedVersion = snapshot.Entries[0].Version
			change.Reset = true
			require.NoError(t, UpdateUserModelPricing(81, []ModelPricingChange{change}))
			rules, err := UserModelPricingFromCache(81)
			require.NoError(t, err)
			assert.Empty(t, rules)
			var original Option
			require.NoError(t, db.Where(commonKeyCol+" = ?", "ModelPrice").First(&original).Error)
			assert.Equal(t, `{"image":0.1}`, original.Value)
		})
	}
}

func TestUserModelPricingPreviewRespectsExplicitCompletionRatio(t *testing.T) {
	draft := PricingValues{"ModelRatio":2.0,"CompletionRatio":3.0,"billing_setting.billing_mode":"ratio"}
	preview,err := PreviewModelPricing("gpt-4",draft,true)
	require.NoError(t,err)
	assert.Equal(t,3.0,preview["CompletionRatio"])
	assert.Equal(t,2.0,preview["ModelRatio"])
}
