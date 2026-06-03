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
		image, port := dbEngineImage(c.engine, "")
		if image != c.image || port != c.port {
			t.Fatalf("engine %s returned %s/%d", c.engine, image, port)
		}
	}
}
