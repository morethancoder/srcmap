package sample

type Server struct {
	Host string
	Port int
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() error {
	return nil
}

type Handler interface {
	Handle(req Request) Response
}

type Request struct {
	Method string
	Path   string
}

type Response struct {
	Status int
	Body   string
}

func NewServer(host string, port int) *Server {
	return &Server{Host: host, Port: port}
}

const DefaultPort = 8080
