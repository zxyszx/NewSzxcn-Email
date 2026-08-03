package app

import "testing"

func TestHasMinimumPasswordLength(t *testing.T) {
	tests := []struct {
		name     string
		password string
		want     bool
	}{
		{name: "five ASCII characters", password: "abc12", want: false},
		{name: "six ASCII characters", password: "abc123", want: true},
		{name: "six Unicode characters", password: "密码测试六位", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasMinimumPasswordLength(tt.password); got != tt.want {
				t.Fatalf("hasMinimumPasswordLength(%q) = %v, want %v", tt.password, got, tt.want)
			}
		})
	}
}
