package networking

import (
	"context"
	"fmt"

	"github.com/lholliger/hive/internal/store"
)

var defaultDBPorts = map[string]int{
	"postgres": 5432,
	"mysql":    3306,
	"redis":    6379,
	"mongo":    27017,
}

func ResolveServiceLinks(ctx context.Context, db *store.Store, appID string) (map[string]string, error) {
	links, err := db.ListServiceLinks(ctx, appID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]string)
	for _, sl := range links {
		prefix := sl.EnvPrefix
		if prefix == "" {
			continue
		}
		key := func(suffix string) string {
			if prefix == "" {
				return suffix
			}
			return prefix + "_" + suffix
		}
		if sl.TargetAppID != "" {
			targetApp, err := db.GetApp(ctx, sl.TargetAppID)
			if err != nil || targetApp == nil {
				continue
			}
			serviceName := fmt.Sprintf("hive-app-%s", targetApp.Name)
			port := targetApp.Port
			if port <= 0 {
				port = 3000
			}
			result[key("HOST")] = serviceName
			result[key("PORT")] = fmt.Sprintf("%d", port)
		} else if sl.TargetDatabaseID != "" {
			dbRes, err := db.GetManagedDatabase(ctx, sl.TargetDatabaseID)
			if err != nil || dbRes == nil {
				continue
			}
			serviceName := fmt.Sprintf("hive-db-%s", dbRes.Name)
			port := defaultDBPorts[dbRes.DBType]
			if port == 0 {
				port = 5432
			}
			result[key("HOST")] = serviceName
			result[key("PORT")] = fmt.Sprintf("%d", port)
			result[key("DATABASE")] = dbRes.Name
		}
	}
	return result, nil
}
