package model

import (
	"sync"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// 多图和长签名链接的清单可能超过 64 KB，媒体归档不应受 MySQL 普通文本列的长度限制。
func TestAsyncRelayMediaManifestUsesLargeTextAcrossDatabases(t *testing.T) {
	parsed, err := schema.Parse(&AsyncRelayTask{}, &sync.Map{}, schema.NamingStrategy{})
	require.NoError(t, err)
	field := parsed.LookUpField("ResultFiles")
	require.NotNil(t, field)
	for _, tc := range []struct {
		name       string
		dialect    gorm.Dialector
		columnType string
	}{
		{"SQLite", sqlite.Open(":memory:"), "text"},
		{"MySQL", mysql.New(mysql.Config{SkipInitializeWithVersion: true}), "longtext"},
		{"PostgreSQL", postgres.New(postgres.Config{}), "text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.columnType, tc.dialect.DataTypeOf(field))
		})
	}
}
