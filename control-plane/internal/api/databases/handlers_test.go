package databases

import "testing"

func TestDBEngineImage(t *testing.T) {
	cases := []struct {
		engine string
		image  string
		port   int
	}{
		{engine: "postgres", image: "postgres:latest", port: 5432},
		{engine: "mysql", image: "mysql:latest", port: 3306},
		{engine: "mongo", image: "mongo:latest", port: 27017},
	}
	for _, c := range cases {
		image, port, ok := dbEngineImage(c.engine, "")
		if !ok || image != c.image || port != c.port {
			t.Fatalf("engine %s returned %s/%d ok=%v", c.engine, image, port, ok)
		}
	}
}

func TestDBEngineImageRejectsUnknown(t *testing.T) {
	if _, _, ok := dbEngineImage("oracle", ""); ok {
		t.Fatal("unknown engine should be rejected")
	}
}

func TestDBServiceEnvUsesSecretFiles(t *testing.T) {
	env := dbServiceEnv("postgres", "app", "db-password", "appdb")
	want := map[string]bool{
		"POSTGRES_USER=app": true,
		"POSTGRES_DB=appdb": true,
		"POSTGRES_PASSWORD_FILE=/run/secrets/db-password": true,
	}
	for _, item := range env {
		delete(want, item)
	}
	if len(want) != 0 {
		t.Fatalf("missing env entries: %#v from %#v", want, env)
	}
}
