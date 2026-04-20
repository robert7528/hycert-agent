package model

import "testing"

func TestTargetDetailNormalize_TrimsPathFields(t *testing.T) {
	d := TargetDetail{
		OS:         "  linux  ",
		CertPath:   "/etc/nginx/ssl/cert.pem ",
		KeyPath:    " /etc/nginx/ssl/key.pem",
		Alias:      "\ttomcat\n",
		ReloadCmd:  "  nginx -s reload  ",
		VerifyHost: " 127.0.0.1 ",
	}
	d.Normalize()

	if d.OS != "linux" {
		t.Errorf("OS = %q, want %q", d.OS, "linux")
	}
	if d.CertPath != "/etc/nginx/ssl/cert.pem" {
		t.Errorf("CertPath = %q, want trailing space stripped", d.CertPath)
	}
	if d.KeyPath != "/etc/nginx/ssl/key.pem" {
		t.Errorf("KeyPath = %q, want leading space stripped", d.KeyPath)
	}
	if d.Alias != "tomcat" {
		t.Errorf("Alias = %q, want tabs/newlines stripped", d.Alias)
	}
	if d.ReloadCmd != "nginx -s reload" {
		t.Errorf("ReloadCmd = %q, want trimmed", d.ReloadCmd)
	}
	if d.VerifyHost != "127.0.0.1" {
		t.Errorf("VerifyHost = %q, want trimmed", d.VerifyHost)
	}
}

func TestTargetDetailNormalize_DoesNotTrimPassword(t *testing.T) {
	d := TargetDetail{
		Password: "  hunter2  ", // surrounding whitespace must be preserved
	}
	d.Normalize()

	if d.Password != "  hunter2  " {
		t.Errorf("Password was modified to %q; keystore passwords must be byte-exact", d.Password)
	}
}

func TestTargetDetailNormalize_EmptyFieldsStayEmpty(t *testing.T) {
	d := TargetDetail{}
	d.Normalize()

	if d.CertPath != "" || d.KeyPath != "" || d.ReloadCmd != "" {
		t.Errorf("empty fields should remain empty, got %+v", d)
	}
}
