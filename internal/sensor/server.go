package sensor

import (
	"fmt"
	"net"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"go.emeland.io/modelsrv/pkg/backend"
	"go.emeland.io/modelsrv/pkg/endpoint"
	"go.emeland.io/modelsrv/pkg/events"
	"go.emeland.io/modelsrv/pkg/model"
	"go.emeland.io/modelsrv/pkg/model/node"
)

// ldapSensorNodeTypeID is the stable identity for the "ldap-sensor" NodeType.
// All instances of this sensor share this UUID. It is a deterministic UUIDv5
// (namespace emeland.io, name "ldap-sensor.nodetype") and must not change
// after the first deployment.
var ldapSensorNodeTypeID = uuid.MustParse("2b8c7ba4-80c4-596b-8abc-0a0f1eaf14e8")

// Server runs a modelsrv web endpoint on top of a modelsrv [backend.Backend].
// The scanner writes Identity and Group resources into the backend's model and
// the backend's event manager forwards the resulting events to subscribers.
type Server struct {
	backend backend.Backend
	nodeID  uuid.UUID
	log     *zap.SugaredLogger
}

// New starts a sensor server bound to listenAddr, pre-registering any provided
// downstream subscriber URLs. selfURL is this sensor's own base API URL as
// seen by its subscribers; it is stamped as the origin of every event.
func New(listenAddr, selfURL string, subscribers []string, log *zap.SugaredLogger) (*Server, error) {
	if log == nil {
		log = zap.NewNop().Sugar()
	}
	if selfURL == "" {
		selfURL = DefaultSelfURL(listenAddr)
	}

	b, err := backend.New(backend.WithLogger(log), backend.WithSelfURL(selfURL))
	if err != nil {
		return nil, fmt.Errorf("create backend: %w", err)
	}
	m := b.GetModel()
	em := b.GetEventManager()

	nt := node.NewNodeType(ldapSensorNodeTypeID)
	nt.SetDisplayName("ldap-sensor")
	if err := m.AddNodeType(nt); err != nil {
		return nil, fmt.Errorf("register node type: %w", err)
	}

	nodeID := uuid.New()
	n := node.NewNode(nodeID)
	n.SetDisplayName("ldap-sensor")
	n.SetNodeTypeByRef(nt)
	if err := m.AddNode(n); err != nil {
		return nil, fmt.Errorf("register node: %w", err)
	}

	// Register downstream subscribers only after NodeType + Node exist so
	// AddSubscriber's replay delivers them reliably.
	for _, s := range subscribers {
		if err := em.AddSubscriber(s); err != nil {
			return nil, fmt.Errorf("add subscriber %q: %w", s, err)
		}
	}

	if err := endpoint.StartWebListener(m, em, listenAddr, endpoint.WebListenerOptions{Logger: log}); err != nil {
		return nil, err
	}

	log.Infow("sensor registered", "nodeTypeId", ldapSensorNodeTypeID, "nodeId", nodeID)
	return &Server{backend: b, nodeID: nodeID, log: log}, nil
}

// Model returns the local model backing this sensor.
func (s *Server) Model() model.Model {
	return s.backend.GetModel()
}

// Addr returns the address the sensor's web endpoint is listening on. This is
// useful when New was given a wildcard port (":0") to discover the bound port.
func (s *Server) Addr() net.Addr {
	return endpoint.WebListenerAddr()
}

// Close shuts down the sensor's HTTP listener and deregisters its Node.
func (s *Server) Close() error {
	if err := s.Model().DeleteNodeById(s.nodeID); err != nil {
		s.log.Warnw("failed to delete node on shutdown", "nodeId", s.nodeID, "error", err)
	}
	endpoint.StopWebListener()
	return nil
}

// Emit applies the event to this process's model; the backend records it and
// forwards it to subscribers.
func (s *Server) Emit(ev events.Event) error {
	return s.Model().Apply(ev)
}

// DefaultSelfURL derives the base API URL from the listen address. It is only
// right when subscribers can reach the sensor at that address; behind a
// Service or with a wildcard bind address, pass an explicit self URL.
func DefaultSelfURL(listenAddr string) string {
	return fmt.Sprintf("http://%s/api/", listenAddr)
}
