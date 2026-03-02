package handlers

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
)

type TaskInfo struct {
	ID        string    `json:"id"`
	NodeID    string    `json:"node_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	Image     string    `json:"image"`
	Slot      int       `json:"slot"`
	CreatedAt time.Time `json:"created_at"`
}

type ServiceEvt struct {
	Action  string    `json:"action"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
}

type PortMapping struct {
	Protocol      string `json:"protocol"`
	TargetPort    uint32 `json:"target_port"`
	PublishedPort uint32 `json:"published_port"`
	PublishMode  string `json:"publish_mode"`
}

func AppTasks(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker client unavailable"})
		return
	}
	defer cli.Close()

	serviceName := "hive-app-" + app.Name
	services, err := cli.ServiceList(r.Context(), swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("name", serviceName)),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(services) == 0 {
		writeJSON(w, http.StatusOK, []TaskInfo{})
		return
	}
	svc := services[0]

	tasks, err := cli.TaskList(r.Context(), swarm.TaskListOptions{
		Filters: filters.NewArgs(filters.Arg("service", svc.ID)),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	result := make([]TaskInfo, 0, len(tasks))
	for _, t := range tasks {
		msg := ""
		if t.Status.Err != "" {
			msg = t.Status.Err
		} else if t.Status.Message != "" {
			msg = t.Status.Message
		}
		img := ""
		if t.Spec.ContainerSpec != nil && t.Spec.ContainerSpec.Image != "" {
			img = t.Spec.ContainerSpec.Image
		}
		result = append(result, TaskInfo{
			ID:        t.ID,
			NodeID:    t.NodeID,
			Status:    string(t.Status.State),
			Message:   msg,
			Image:     img,
			Slot:      int(t.Slot),
			CreatedAt: t.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func AppEvents(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker client unavailable"})
		return
	}
	defer cli.Close()

	serviceName := "hive-app-" + app.Name
	f := filters.NewArgs(
		filters.Arg("type", "service"),
		filters.Arg("service", serviceName),
	)
	since := time.Now().Add(-1 * time.Hour)
	until := time.Now()
	msgCh, errCh := cli.Events(r.Context(), events.ListOptions{
		Filters: f,
		Since:   since.Format(time.RFC3339Nano),
		Until:   until.Format(time.RFC3339Nano),
	})

	var result []ServiceEvt
	collectDone := make(chan struct{})
	go func() {
		for msg := range msgCh {
			action := string(msg.Action)
			imgName := msg.Actor.Attributes["image"]
			result = append(result, ServiceEvt{
				Action:  action,
				Message: imgName + " " + action,
				Time:    time.Unix(0, msg.TimeNano),
			})
		}
		close(collectDone)
	}()

	select {
	case <-collectDone:
	case err := <-errCh:
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	case <-r.Context().Done():
	}

	if result == nil {
		result = []ServiceEvt{}
	}
	writeJSON(w, http.StatusOK, result)
}

func AppPorts(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker client unavailable"})
		return
	}
	defer cli.Close()

	serviceName := "hive-app-" + app.Name
	services, err := cli.ServiceList(r.Context(), swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("name", serviceName)),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(services) == 0 {
		writeJSON(w, http.StatusOK, []PortMapping{})
		return
	}
	svc := services[0]
	svcDetail, _, err := cli.ServiceInspectWithRaw(r.Context(), svc.ID, swarm.ServiceInspectOptions{})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var result []PortMapping
	if len(svcDetail.Endpoint.Ports) > 0 {
		for _, p := range svcDetail.Endpoint.Ports {
			result = append(result, PortMapping{
				Protocol:      string(p.Protocol),
				TargetPort:    p.TargetPort,
				PublishedPort: p.PublishedPort,
				PublishMode:   string(p.PublishMode),
			})
		}
	}
	if len(result) == 0 && svcDetail.Spec.EndpointSpec != nil && len(svcDetail.Spec.EndpointSpec.Ports) > 0 {
		for _, p := range svcDetail.Spec.EndpointSpec.Ports {
			result = append(result, PortMapping{
				Protocol:      string(p.Protocol),
				TargetPort:    p.TargetPort,
				PublishedPort: p.PublishedPort,
				PublishMode:   string(p.PublishMode),
			})
		}
	}
	if result == nil {
		result = []PortMapping{}
	}
	writeJSON(w, http.StatusOK, result)
}
