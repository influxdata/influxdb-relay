package relay

import (
	log "github.com/Sirupsen/logrus"
	"github.com/tecbot/gorocksdb"
)

type rocksdb struct {
	opts *gorocksdb.Options
	db   *gorocksdb.DB
	ro   *gorocksdb.ReadOptions
	wo   *gorocksdb.WriteOptions
}

func newRocksdb(path string) *rocksdb {
	r := new(rocksdb)
	r.opts = gorocksdb.NewDefaultOptions()

	//opts.SetBlockCache(gorocksdb.NewLRUCache(3 << 20))
	r.opts.SetCreateIfMissing(true)

	r.openDb(path)
	r.newRead()
	r.newWrite()

	return r
}

func (r *rocksdb) openDb(path string) {
	db, err := gorocksdb.OpenDb(r.opts, path)
	if err != nil {
		log.Fatal("open db error", err)
	}

	r.db = db
}

func (r *rocksdb) newRead() {
	r.ro = gorocksdb.NewDefaultReadOptions()
	r.ro.SetFillCache(false)
}

func (r *rocksdb) read() (string, string) {
	//Get data one by one
	it := r.db.NewIterator(r.ro)
	defer it.Close()

	it.SeekToFirst()
	if !it.Valid() {
		return "", ""
	}

	key := it.Key()
	value := it.Value()

	defer key.Free()
	defer value.Free()

	if err := it.Err(); err != nil {
		log.Error("Error:", err)
	}

	return string(key.Data()[:]), string(value.Data()[:])
}

func (r *rocksdb) newWrite() {
	r.wo = gorocksdb.NewDefaultWriteOptions()
	r.wo.SetSync(true)
}

func (r *rocksdb) write(key, value string) error {
	err := r.db.Put(r.wo, []byte(key), []byte(value))

	return err
}

func (r *rocksdb) remove(key string) error {
	err := r.db.Delete(r.wo, []byte(key))

	return err
}

func (r *rocksdb) destroy() {
	r.wo.Destroy()
	r.ro.Destroy()
	r.db.Close()
	r.opts.Destroy()
}
