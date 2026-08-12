package graph

import "testing"

func TestValidateMicrosoftURL(t *testing.T) {
	valid := []string{
		"https://graph.microsoft.com/v1.0/me",
		"https://teams.microsoft.com/api/chatsvc/amer/v1/users/ME/conversations/48:notifications/messages?backwardLink=x",
		"https://assignments.edu.cloud.microsoft/api/v1.0/edu/me/work",
		"https://contoso-my.sharepoint.com/personal/a",
		"https://contoso.sharepoint.com/:b:/r/file.docx",
		"https://graph.microsoft.com",
	}
	for _, u := range valid {
		if err := ValidateMicrosoftURL(u); err != nil {
			t.Errorf("ValidateMicrosoftURL(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{
		"",
		"http://graph.microsoft.com/v1.0/me", // not https
		"ftp://graph.microsoft.com/file",     // wrong scheme
		"https://evil.example.com/steal",     // non-Microsoft host
		"https://microsoft.com.evil.example.com/x",     // suffix trick
		"https://graph.microsoft.com@evil.example.com", // userinfo trick
		"file:///etc/passwd",                           // local file
		"https://",                                     // no host
	}
	for _, u := range invalid {
		if err := ValidateMicrosoftURL(u); err == nil {
			t.Errorf("ValidateMicrosoftURL(%q) = nil, want error", u)
		}
	}
}

func TestValidateMediaURL(t *testing.T) {
	valid := []string{
		"https://us-api.asm.skype.com/v1/objects/abc/views/imgo",
		"https://media.skypeassets.com/image.png",
		"https://contoso.akamaihd.net/img.png",
		"https://teams.microsoft.com/image.png",
	}
	for _, u := range valid {
		if err := validateMediaURL(u); err != nil {
			t.Errorf("validateMediaURL(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{
		"https://attacker.example.com/leak.png",
		"http://us-api.asm.skype.com/image.png",
		"https://",
		"javascript:alert(1)",
	}
	for _, u := range invalid {
		if err := validateMediaURL(u); err == nil {
			t.Errorf("validateMediaURL(%q) = nil, want error", u)
		}
	}
}
