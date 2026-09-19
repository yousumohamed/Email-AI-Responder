package email

import (
	"testing"
)

func TestCheckFilters(t *testing.T) {
	myEmail := "bot@company.com"

	tests := []struct {
		name         string
		msg          *Message
		expectIgnore bool
	}{
		{
			name: "Regular customer inquiry",
			msg: &Message{
				From:    "customer@gmail.com",
				Subject: "Question regarding product",
				Headers: map[string][]string{},
			},
			expectIgnore: false,
		},
		{
			name: "Own email loop prevention",
			msg: &Message{
				From:    "bot@company.com",
				Subject: "Self email",
			},
			expectIgnore: true,
		},
		{
			name: "Own email with name",
			msg: &Message{
				From:    "Email Bot <bot@company.com>",
				Subject: "Self test",
			},
			expectIgnore: true,
		},
		{
			name: "No-reply address",
			msg: &Message{
				From:    "noreply@service.com",
				Subject: "Update",
			},
			expectIgnore: true,
		},
		{
			name: "Do-not-reply address",
			msg: &Message{
				From:    "do-not-reply@updates.org",
				Subject: "Newsletter",
			},
			expectIgnore: true,
		},
		{
			name: "Auto-submitted header",
			msg: &Message{
				From:    "user@domain.com",
				Subject: "Auto reply",
				Headers: map[string][]string{
					"Auto-Submitted": {"auto-generated"},
				},
			},
			expectIgnore: true,
		},
		{
			name: "Precedence bulk header",
			msg: &Message{
				From:    "info@marketing.com",
				Subject: "Weekly digest",
				Headers: map[string][]string{
					"Precedence": {"bulk"},
				},
			},
			expectIgnore: true,
		},
		{
			name: "Mailing list header",
			msg: &Message{
				From:    "member@list.com",
				Subject: "Discussion",
				Headers: map[string][]string{
					"List-Id": {"<announcements.list.com>"},
				},
			},
			expectIgnore: true,
		},
		{
			name: "Delivery failure bounce subject",
			msg: &Message{
				From:    "system@domain.com",
				Subject: "Delivery Status Notification (Failure)",
			},
			expectIgnore: true,
		},
		{
			name: "Mailer-Daemon sender",
			msg: &Message{
				From:    "Mailer-Daemon <MAILER-DAEMON@mail.com>",
				Subject: "Undelivered mail",
			},
			expectIgnore: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := CheckFilters(tt.msg, myEmail, true, true, true)
			if res.ShouldIgnore != tt.expectIgnore {
				t.Errorf("CheckFilters() for %s: got ShouldIgnore = %v (reason: %s), want %v",
					tt.name, res.ShouldIgnore, res.Reason, tt.expectIgnore)
			}
		})
	}
}
