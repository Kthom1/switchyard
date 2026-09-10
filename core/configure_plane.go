package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func (a Installation) configurePlane() error {
	if err := a.Settings.Validate(); err != nil {
		return err
	}
	origin, _ := url.Parse(a.Settings.WebURL)
	values := [][2]string{
		{"APP_RELEASE", "v1.4.2"}, {"APP_DOMAIN", origin.Host}, {"WEB_URL", a.Settings.WebURL},
		{"CORS_ALLOWED_ORIGINS", a.Settings.WebURL + ",http://localhost:8090"}, {"SITE_ADDRESS", ":80"},
		{"CERT_EMAIL", ""}, {"CERT_ACME_CA", "https://acme-v02.api.letsencrypt.org/directory"},
		{"CERT_ACME_DNS", ""}, {"API_KEY_RATE_LIMIT", "600/minute"}, {"AWS_ACCESS_KEY_ID", "switchyard"},
	}
	secrets := make(map[string]string)
	for _, name := range []string{"POSTGRES_PASSWORD", "RABBITMQ_PASSWORD", "SECRET_KEY", "LIVE_SERVER_SECRET_KEY", "AWS_SECRET_ACCESS_KEY"} {
		var bytes [32]byte
		if _, err := rand.Read(bytes[:]); err != nil {
			return err
		}
		secrets[name] = hex.EncodeToString(bytes[:])
		values = append(values, [2]string{name, secrets[name]})
	}
	values = append(values,
		[2]string{"DATABASE_URL", "postgresql://plane:" + secrets["POSTGRES_PASSWORD"] + "@plane-db/plane"},
		[2]string{"AMQP_URL", "amqp://plane:" + secrets["RABBITMQ_PASSWORD"] + "@plane-mq:5672/plane"},
	)
	var data strings.Builder
	for _, pair := range values {
		fmt.Fprintf(&data, "%s=%s\n", pair[0], pair[1])
	}
	return writeNew(filepath.Join(a.Root, ".env.plane"), []byte(data.String()), 0600)
}
