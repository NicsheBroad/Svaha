package web

import (
	"fmt"
	"testing"
	"time"
)

// TestLoginLimits проверяет общие квоты по логину и IP, в том числе при смене исходящего порта.
func TestLoginLimits(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	var byUsername, byAddress loginLimiter
	for i := 0; i < 8; i++ {
		if wait := byUsername.allow(now, fmt.Sprintf("192.0.2.%d:1234", i+1), "alice"); wait != 0 {
			t.Fatalf("попытка %d по логину преждевременно ограничена", i+1)
		}
	}
	if wait := byUsername.allow(now, "192.0.2.99:1234", "alice"); wait != loginWindow {
		t.Fatalf("попытка с новым IP обошла лимит по логину: %v", wait)
	}
	if wait := byUsername.allow(now.Add(loginWindow), "192.0.2.99:1234", "alice"); wait != 0 {
		t.Fatalf("лимит по логину не сбросился: %v", wait)
	}

	for i := 0; i < 30; i++ {
		if wait := byAddress.allow(now, fmt.Sprintf("192.0.2.1:%d", 2000+i), fmt.Sprintf("login-%d", i)); wait != 0 {
			t.Fatalf("попытка %d с одного IP преждевременно ограничена", i+1)
		}
	}
	if wait := byAddress.allow(now, "192.0.2.1:3000", "new-login"); wait != loginWindow {
		t.Fatalf("попытка с новым логином обошла лимит по IP: %v", wait)
	}
}

// TestAdultAge проверяет допуск в день восемнадцатилетия и отказ накануне.
func TestAdultAge(t *testing.T) {
	now := time.Date(2026, 11, 20, 0, 15, 0, 0, time.FixedZone("UTC+5", 5*60*60))
	birthday := time.Date(2008, 11, 20, 0, 0, 0, 0, time.UTC)
	if !isAdult(birthday, now) {
		t.Fatal("день восемнадцатилетия должен быть разрешён")
	}
	if isAdult(birthday.AddDate(0, 0, 1), now) {
		t.Fatal("до восемнадцатилетия доступ должен быть запрещён")
	}
}
