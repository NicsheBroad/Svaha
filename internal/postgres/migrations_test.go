package postgres

import (
	"strings"
	"testing"
)

// TestMigrationHistory проверяет контрольные суммы и обнаружение пропавших миграций.
func TestMigrationHistory(t *testing.T) {
	migrations, err := loadMigrations()
	if err != nil || len(migrations) == 0 {
		t.Fatalf("не удалось загрузить миграции: %v", err)
	}
	first := migrations[0]
	if len(first.checksum) != 64 {
		t.Fatal("ожидалась контрольная сумма SHA-256")
	}
	for i := 1; i < len(migrations); i++ {
		if migrations[i].version <= migrations[i-1].version {
			t.Fatal("миграции должны загружаться по порядку")
		}
	}

	for _, example := range []struct {
		applied map[string]string
		failure string
	}{
		{map[string]string{}, ""},
		{map[string]string{first.version: ""}, ""},
		{map[string]string{first.version: first.checksum}, ""},
		{map[string]string{first.version: "changed"}, first.version},
		{map[string]string{"missing.sql": "unknown"}, "отсутствующие"},
	} {
		err := validateMigrationHistory(migrations, example.applied)
		if example.failure == "" {
			if err != nil {
				t.Errorf("корректная история отклонена: %v", err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), example.failure) {
			t.Errorf("ожидалась ошибка %q, получено %v", example.failure, err)
		}
	}
}
