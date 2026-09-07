package utils

import "testing"

func TestValidateHumanName_RejectsSpam(t *testing.T) {
	bad := []string{
		"Поздравляем! Вы получили замечательный вознаграждение https://tinyurl.com/mse5t79y#vdnsu6",
		"Вау! Личный выигрыш готов для вас! Изучите по ссылке https://tinyurl.com/mr2tuzjj",
		"Congratulations! You have won a gift card, click here",
		"John www.example.com",
		"visit bit.ly/abc now",
		"name@example.com",
		"!!!! ??? ####",
		"日本語のなまえ",
		"",
		"a",
		"Смирнов Иван", // Cyrillic-only, no link — still spam on this site
	}
	for _, name := range bad {
		if ok, _ := ValidateHumanName(name); ok {
			t.Errorf("expected %q to be rejected", name)
		}
	}
}

func TestValidateHumanName_AcceptsReal(t *testing.T) {
	good := []string{
		"محمد عبدالله",
		"قصي الدجة",
		"Ahmed Al-Rashid",
		"Jean-Pierre O'Brien",
		"Sara",
		"عبد الرحمن بن عوف",
		"Mary Jane Watson",
	}
	for _, name := range good {
		if ok, reason := ValidateHumanName(name); !ok {
			t.Errorf("expected %q to be accepted, got reason %q", name, reason)
		}
	}
}

func TestNormalizeHumanName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Ahmed   Ali  ", "Ahmed Ali"},
		{"line\nbreak", "line break"},
		{"tab\tsep", "tab sep"},
		{"drop\x00ctl", "dropctl"},
	}
	for _, c := range cases {
		if got := NormalizeHumanName(c.in); got != c.want {
			t.Errorf("NormalizeHumanName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
