package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	port := flag.Int("port", 8080, "Port to serve on")
	flag.Parse()

	servers := []string{
		"http://localhost:8081",
		"http://localhost:8082",
		"http://localhost:8083",
		"http://localhost:8084",
		"http://localhost:8085",
		"http://localhost:8086",
		"http://localhost:8087",
		"http://localhost:8088",
		"http://localhost:8089",
	}

	lb := &LoadBalancer{}

	for _, surl := range servers {
		url, err := url.Parse(surl)
		if err != nil {
			log.Fatal(err)
		}

		proxy := httputil.NewSingleHostReverseProxy(url)
		proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("Error response from proxy: %v", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
		}

		lb.backends = append(lb.backends, &BackEnd{
			mux:    sync.Mutex{},
			RProxy: *proxy,
			url:    url,
		})
		log.Printf("Configured server on port %s", url)
	}

	lb.healthCheck()

	go lb.PeriodicHealthCheck(time.Minute)

	server := http.Server{
		Addr:    fmt.Sprintf(":%d", *port),
		Handler: lb,
	}

	log.Printf("Load balancer started on port :%d\n", *port)
	err := server.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}

type BackEnd struct {
	url    *url.URL
	alive  bool
	mux    sync.Mutex
	RProxy httputil.ReverseProxy
}

func (b *BackEnd) isAlive() bool {
	b.mux.Lock()
	defer b.mux.Unlock()
	return b.alive
}

func (b *BackEnd) setAlive(alive bool) {
	b.mux.Lock()
	defer b.mux.Unlock()
	b.alive = alive
}

type LoadBalancer struct {
	backends []*BackEnd
	counter  uint64
}

func (l *LoadBalancer) nextBackend() *BackEnd {
	//Setup next index based on current counter
	next := atomic.AddUint64(&l.counter, uint64(1)) % uint64(len(l.backends))

	//Find the next healthy backend servers
	for i := 0; i < len(l.backends); i++ {
		idx := (int(next) + i) % len(l.backends)
		if l.backends[idx].isAlive() {
			return l.backends[idx]
		}
	}

	return nil
}

func (b *BackEnd) isBackendAlive() bool {
	timeout := 5 * time.Second
	conn, err := net.DialTimeout("tcp", b.url.Host, timeout)
	if err != nil {
		log.Printf("Site unreachable on port %s", err)
		b.setAlive(false)
		return false
	}
	defer conn.Close()
	return true
}

func (l *LoadBalancer) healthCheck() {
	for _, b := range l.backends {
		status := b.isBackendAlive()
		b.setAlive(status)
		if status {
			log.Printf("Service on port %s is doing well", b.url.String())
		} else {
			log.Printf("Service on port %s is dead", b.url.String())
		}
	}
}

func (l *LoadBalancer) PeriodicHealthCheck(interval time.Duration) {
	t := time.NewTicker(interval)
	<-t.C
	l.healthCheck()
}

func (l *LoadBalancer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b := l.nextBackend()
	if b == nil {
		http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
		return
	}

	b.RProxy.ServeHTTP(w, r)
}
