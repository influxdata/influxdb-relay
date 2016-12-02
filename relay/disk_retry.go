package relay

import (
	"strings"
	"sync"
	"time"
	"fmt"

	log "github.com/Sirupsen/logrus"
)

type retryDb struct {
	initialInterval time.Duration
	multiplier      time.Duration
	maxInterval     time.Duration

	db *rocksdb
	p  poster

	wg sync.WaitGroup

	resp *responseData
}

func newDiskRetryBuffer(post poster, prefix, suffix string) *retryDb {
	path := format(prefix, suffix)
	log.Info("Insert data to ", path)

	err := Mkdir(path)
	if err != nil {
		log.Error("mkdir error", err)
	}

	data := newRocksdb(path)

	r := &retryDb{
		initialInterval: retryInitial,
		multiplier:      retryMultiplier,
		maxInterval:     DefaultMaxDelayInterval,
		db:              data,
		p:               post,
	}

	go r.run()
	return r
}

func (r *retryDb) post(buf []byte, query string) (*responseData, error) {
	resp, err := r.p.post(buf, query)
	// TODO A 5xx caused by the point data could cause the relay to buffer forever
	if err == nil && resp.StatusCode/100 != 5 {
		return resp, err
	}

	// already buffering or failed request
	err = add(r.db, buf, query)
	if err != nil {
		return nil, err
	}

	r.wg.Add(1)
	r.wg.Wait()

	return r.resp, nil
}

func (r *retryDb) run() {
	defer r.db.destroy()

	for {
		interval := r.initialInterval

		key, value := pop(r.db)
		if key == "" && value == "" {
			time.Sleep(interval)
			continue
		}

		for {
			resp, err := r.p.post([]byte(key), value)
			if err == nil && resp.StatusCode/100 != 5 {
				r.resp = resp
				r.wg.Done()
				break
			}

			if interval != r.maxInterval {
				interval *= r.multiplier
				if interval > r.maxInterval {
					interval = r.maxInterval
				}
			}

			time.Sleep(interval)
		}
	}
}

// pop will remove and return the first element of the list, blocking if necessary
func pop(db *rocksdb) (body, option string) {
	var s []string

	key, value := db.read()
	if key != "" {
		str:=fmt.Sprintf("pop: key=%v, value=%v", key, value)
		log.Debug(str)
	}

	err := db.remove(key)
	if err != nil {
		log.Error(key, "remove error")
	}

	s = strings.Split(value, "|")

	body = s[0]
	option = s[len(s)-1]

	return
}

func add(db *rocksdb, buf []byte, query string) (err error) {
	var s []string

	body := string(buf[:])

	s = strings.Split(body, " ")
	key := s[len(s)-1]

	err = db.write(key, body+"|"+query)
	if err != nil {
		return err
	}

	return
}
