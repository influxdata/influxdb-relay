package relay

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/influxdata/influxdb/models"
)

// HTTP is a relay for HTTP influxdb writes
type HTTP struct {
	addr string
	name string

	closing int64
	l       net.Listener

	backends []*httpBackend
}

const (
	DefaultHTTPTimeout      = 10 * time.Second
	DefaultMaxDelayInterval = 10 * time.Second
	DefaultInitialInterval  = 500 * time.Millisecond
	DefaultMultiplier       = 2
)

func NewHTTP(cfg HTTPConfig) (Relay, error) {
	h := new(HTTP)

	h.addr = cfg.Addr
	h.name = cfg.Name

	for i := range cfg.Outputs {
		b := &cfg.Outputs[i]

		if b.Name == "" {
			b.Name = b.Location
		}
		timeout := DefaultHTTPTimeout
		if b.Timeout != "" {
			t, err := time.ParseDuration(b.Timeout)
			if err != nil {
				return nil, fmt.Errorf("error parsing HTTP timeout %v", err)
			}
			timeout = t
		}
		// If configured, create a retryBuffer per backend.
		// This way we serialize retries against each backend.
		var rb *retryBuffer
		if b.BufferSize > 0 {
			max := DefaultMaxDelayInterval
			if b.MaxDelayInterval != "" {
				m, err := time.ParseDuration(b.MaxDelayInterval)
				if err != nil {
					return nil, fmt.Errorf("error parsing max retry time %v", err)
				}
				max = m
			}
			rb = newRetryBuffer(b.BufferSize, DefaultInitialInterval, DefaultMultiplier, max)
		}

		h.backends = append(h.backends, &httpBackend{
			name:          b.Name,
			location:      b.Location,
			retryBuffer:   rb,
			client: &http.Client{
				Timeout: timeout,
			},
		})
	}

	return h, nil
}

func (h *HTTP) Name() string {
	if h.name == "" {
		return "http://" + h.addr
	}
	return h.name
}

func (h *HTTP) Run() error {
	l, err := net.Listen("tcp", h.addr)
	if err != nil {
		return err
	}
	h.l = l

	log.Printf("Starting HTTP relay %q on %v", h.Name(), h.addr)

	err = http.Serve(l, h)
	if atomic.LoadInt64(&h.closing) != 0 {
		return nil
	}
	return err
}

func (h *HTTP) Stop() error {
	atomic.StoreInt64(&h.closing, 1)
	return h.l.Close()
}

func (h *HTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	if r.URL.Path != "/write" {
		jsonError(w, http.StatusNotFound, "invalid write endpoint")
		return
	}

	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
		} else {
			jsonError(w, http.StatusMethodNotAllowed, "invalid write method")
		}
		return
	}

	// fail early if we're missing the database
	if r.URL.Query().Get("db") == "" {
		jsonError(w, http.StatusBadRequest, "missing parameter: db")
		return
	}

	var body = r.Body

	if r.Header.Get("Content-Encoding") == "gzip" {
		b, err := gzip.NewReader(r.Body)
		if err != nil {
			jsonError(w, http.StatusBadRequest, "unable to decode gzip body")
		}
		defer b.Close()
		body = b
	}

	bodyBuf := getBuf()
	_, err := bodyBuf.ReadFrom(body)
	if err != nil {
		putBuf(bodyBuf)
		jsonError(w, http.StatusInternalServerError, "problem reading request body")
		return
	}

	precision := r.URL.Query().Get("precision")
	points, err := models.ParsePointsWithPrecision(bodyBuf.Bytes(), start, precision)
	if err != nil {
		putBuf(bodyBuf)
		jsonError(w, http.StatusBadRequest, "unable to parse points")
		return
	}

	outBuf := getBuf()
	for _, p := range points {
		if _, err = outBuf.WriteString(p.PrecisionString(precision)); err != nil {
			break
		}
		if err = outBuf.WriteByte('\n'); err != nil {
			break
		}
	}

	// done with the input points
	putBuf(bodyBuf)

	if err != nil {
		putBuf(outBuf)
		jsonError(w, http.StatusInternalServerError, "problem writing points")
		return
	}

	defer putBuf(outBuf)
	outBytes := outBuf.Bytes()

	var wg sync.WaitGroup
	wg.Add(len(h.backends))

	var responses = make(chan *http.Response, len(h.backends))

	for _, b := range h.backends {
		b := b
		go func() {
			defer wg.Done()
			username, password, _ := r.BasicAuth()
			resp, err := b.post(outBytes, r.URL.RawQuery, username, password)
			if err != nil {
				log.Printf("Problem posting to relay %q backend %q: %v", h.Name(), b.name, err)
				responses <- nil
			} else {
				if resp.StatusCode/100 != 2 {
					log.Printf("Non-2xx response for relay %q backend %q: %v", h.Name(), b.name, resp.StatusCode)
				}
				responses <- resp
			}
		}()
	}

	go func() {
		wg.Wait()
		close(responses)
	}()

	var responded bool
	var errResponse *http.Response

	for resp := range responses {
		if resp == nil {
			continue
		}

		if responded {
			resp.Body.Close()
			continue
		}

		if resp.StatusCode/100 == 2 { // points written successfully
			w.WriteHeader(http.StatusNoContent)
			responded = true
			resp.Body.Close()
			continue
		}

		if errResponse != nil {
			resp.Body.Close()
			continue
		}

		// hold on to one of the responses to return back to the client
		errResponse = resp
	}

	if responded {
		// at least one success
		if errResponse != nil {
			errResponse.Body.Close()
		}
		return
	}

	// no successful writes
	if errResponse == nil {
		// failed to make any valid request... network error?
		jsonError(w, http.StatusInternalServerError, "unable to write points")
		return
	}

	// errResponse has our answer...
	for _, s := range []string{"Content-Type", "Content-Length", "Content-Encoding"} {
		if v := errResponse.Header.Get(s); v != "" {
			w.Header().Set(s, v)
		}
	}
	w.WriteHeader(errResponse.StatusCode)
	io.Copy(w, errResponse.Body)
	errResponse.Body.Close()
}

var bufPool = sync.Pool{New: func() interface{} { return new(bytes.Buffer) }}

func getBuf() *bytes.Buffer {
	return bufPool.Get().(*bytes.Buffer)
}

func putBuf(b *bytes.Buffer) {
	b.Reset()
	bufPool.Put(b)
}

func jsonError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	data := fmt.Sprintf("{\"error\":%q}\n", message)
	w.Header().Set("Content-Length", fmt.Sprint(len(data)))
	w.WriteHeader(code)
	w.Write([]byte(data))
}

type httpBackend struct {
	name        string
	location    string
	retryBuffer *retryBuffer
	client      *http.Client
	buffering   int32
}

func (b *httpBackend) post(buf []byte, query, username, password string) (response *http.Response, err error) {
	req, err := http.NewRequest("POST", b.location, bytes.NewReader(buf))
	if err != nil {
		return
	}

	req.URL.RawQuery = query
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Content-Length", fmt.Sprint(len(buf)))

	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}

	// Check if we are in buffering mode
	if atomic.LoadInt32(&b.buffering) == 0 {
		// Do action once
		response, err = b.client.Do(req)
		if err == nil {
			return
		}
	}
	if b.retryBuffer != nil {
		// We failed start retry logic if we have a buffer
		err = b.retryBuffer.Retry(func() error {
			// Re-initialize the request body
			req.Body = ioutil.NopCloser(bytes.NewReader(buf))
			// Do request again
			r, err := b.client.Do(req)
			// Retry transport errors and 500s
			if err != nil || r.StatusCode/100 == 5 {
				// Set buffering to 1 since we had a failure
				atomic.StoreInt32(&b.buffering, 1)
				if err == nil {
					err = fmt.Errorf("http code: %d", r.StatusCode)
				}
				return err
			}
			response = r
			// Set buffering to 0 since we had a success
			atomic.StoreInt32(&b.buffering, 0)
			return nil
		})
	}
	return
}
