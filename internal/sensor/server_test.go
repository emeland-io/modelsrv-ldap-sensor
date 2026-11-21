package sensor_test

import (
	"testing"

	"emeland.io/modelsrv-ldap-sensor/internal/sensor"
)

func TestDefaultSelfURL(t *testing.T) {
	if got, want := sensor.DefaultSelfURL("localhost:24300"), "http://localhost:24300/api/"; got != want {
		t.Errorf("DefaultSelfURL = %q, want %q", got, want)
	}
}
