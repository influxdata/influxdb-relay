# InfluxDB Relay

This project adds a basic high availability layer to InfluxDB. With the right architecture and disaster recovery processes, this achieves a highly available setup.

*NOTE:* `influxdb-relay` must be built with Go 1.5+

## Usage

To build from source and run:

```sh
$ # Install influxdb-relay to your $GOPATH/bin
$ go get -u github.com/influxdata/influxdb-relay
$ # Edit your configuration file
$ cp $GOPATH/src/github.com/influxdata/influxdb-relay/sample.toml ./relay.toml
$ vim relay.toml
$ # Start relay!
$ $GOPATH/bin/influxdb-relay -config relay.toml
```

## Configuration

```toml
[[http]]
# Name of the HTTP server, used for display purposes only.
name = "example-http"

# TCP address to bind to, for HTTP server.
bind-addr = "127.0.0.1:9096"

# Enable HTTPS requests.
ssl-combined-pem = "/etc/ssl/influxdb-relay.pem"

# Array of InfluxDB instances to use as backends for Relay.
output = [
    # name: name of the backend, used for display purposes only.
    # location: full URL of the /write endpoint of the backend
    # timeout: Go-parseable time duration. Fail writes if incomplete in this time.
    # skip-tls-verification: skip verification for HTTPS location. WARNING: it's insecure. Don't use in production.
    { name="local1", location="http://127.0.0.1:8086/write", timeout="10s" },
    { name="local2", location="http://127.0.0.1:7086/write", timeout="10s" },
]

[[udp]]
# Name of the UDP server, used for display purposes only.
name = "example-udp"

# UDP address to bind to.
bind-addr = "127.0.0.1:9096"

# Socket buffer size for incoming connections.
read-buffer = 0 # default

# Precision to use for timestamps
precision = "n" # Can be n, u, ms, s, m, h

# Array of InfluxDB instances to use as backends for Relay.
output = [
    # name: name of the backend, used for display purposes only.
    # location: host and port of backend.
    # mtu: maximum output payload size
    { name="local1", location="127.0.0.1:8089", mtu=512 },
    { name="local2", location="127.0.0.1:7089", mtu=1024 },
]
```

## Description

The architecture is fairly simple and consists of a load balancer, two or more InfluxDB Relay processes and two or more InfluxDB processes. The load balancer should point UDP traffic and HTTP POST requests with the path `/write` to the two relays while pointing GET requests with the path `/query` to the two InfluxDB servers.

The setup should look like this:

```
        ┌─────────────────┐                 
        │writes & queries │                 
        └─────────────────┘                 
                 │                          
                 ▼                          
         ┌───────────────┐                  
         │               │                  
┌────────│ Load Balancer │─────────┐        
│        │               │         │        
│        └──────┬─┬──────┘         │        
│               │ │                │        
│               │ │                │        
│        ┌──────┘ └────────┐       │        
│        │ ┌─────────────┐ │       │┌──────┐
│        │ │/write or UDP│ │       ││/query│
│        ▼ └─────────────┘ ▼       │└──────┘
│  ┌──────────┐      ┌──────────┐  │        
│  │ InfluxDB │      │ InfluxDB │  │        
│  │ Relay    │      │ Relay    │  │        
│  └──┬────┬──┘      └────┬──┬──┘  │        
│     │    |              |  │     │        
│     |  ┌─┼──────────────┘  |     │        
│     │  │ └──────────────┐  │     │        
│     ▼  ▼                ▼  ▼     │        
│  ┌──────────┐      ┌──────────┐  │        
│  │          │      │          │  │        
└─▶│ InfluxDB │      │ InfluxDB │◀─┘        
   │          │      │          │           
   └──────────┘      └──────────┘           
 ```



The relay will listen for HTTP or UDP writes and write the data to each InfluxDB server via the HTTP write or UDP endpoint, as appropriate. If the write is sent via HTTP, the relay will return a success response as soon as one of the InfluxDB servers returns a success. If any InfluxDB server returns a 4xx response, that will be returned to the client immediately. If all servers return a 5xx, a 5xx will be returned to the client. If some but not all servers return a 5xx that will not be returned to the client. You should monitor each instance's logs for 5xx errors.

With this setup a failure of one Relay or one InfluxDB can be sustained while still taking writes and serving queries. However, the recovery process might require operator intervention.

## Buffering

The relay can be configured to buffer failed requests for HTTP backends.
The intent of this logic is reduce the number of failures during short outages or periodic network issues.
> This retry logic is **NOT** sufficient for for long periods of downtime as all data is buffered in RAM

Buffering has the following configuration options (configured per HTTP backend):

* buffer-size-mb -- An upper limit on how much point data to keep in memory (in MB)
* max-batch-kb -- A maximum size on the aggregated batches that will be submitted (in KB)
* max-delay-interval -- the max delay between retry attempts per backend.
    The initial retry delay is 500ms and is doubled after every failure.

If the buffer is full then requests are dropped and an error is logged.
If a requests makes it into the buffer it is retried until success.

Retries are serialized to a single backend. In addition, writes will be aggregated and batched as long as the body of the request will be less than `max-batch-kb`
If buffered requests succeed then there is no delay between subsequent attempts.

If the relay stays alive the entire duration of a downed backend server without filling that server's allocated buffer, and the relay can stay online until the entire buffer is flushed, it would mean that no operator intervention would be required to "recover" the data. The data will simply be batched together and written out to the recovered server in the order it was received.

*NOTE*: The limits for buffering are not hard limits on the memory usage of the application, and there will be additional overhead that would be much more challenging to account for. The limits listed are just for the amount of point line protocol (including any added timestamps, if applicable). Factors such as small incoming batch sizes and a smaller max batch size will increase the overhead in the buffer. There is also the general application memory overhead to account for. This means that a machine with 2GB of memory should not have buffers that sum up to _almost_ 2GB.
The buffering feature will only be activated when at least two InfluxDB backends are configured. In addition always at least one backend has to be active for buffering to work.

## Recovery

InfluxDB organizes its data on disk into logical blocks of time called shards. We can use this to create a hot recovery process with zero downtime.

The length of time that shards represent in InfluxDB are typically 1 hour, 1 day, or 7 days, depending on the retention duration, but can be explicitly set when creating the retention policy. For the sake of our example, let's assume shard durations of 1 day.

Let's say one of the InfluxDB servers goes down for an hour on 2016-03-10. Once midnight UTC rolls over, all InfluxDB processes are now writing data to the shard for 2016-03-11 and the file(s) for 2016-03-10 have gone cold for writes. We can then restore things using these steps:

1. Tell the load balancer to stop sending query traffic to the server that was down (this should be done as soon as an outage is detected to prevent partial or inconsistent query returns.)
2. Create backup of 2016-03-10 shard from a server that was up the entire day
3. Restore the backup of the shard from the good server to the server that had downtime
4. Tell the load balancer to resume sending queries to the previously downed server

During this entire process the Relays should be sending current writes to all servers, including the one with downtime.

## Sharding

It's possible to add another layer on top of this kind of setup to shard data. Depending on your needs you could shard on the measurement name or a specific tag like `customer_id`. The sharding layer would have to service both queries and writes.

As this relay does not handle queries, it will not implement any sharding logic. Any sharding would have to be done externally to the relay.


## Caveats

While `influxdb-relay` does provide some level of high availability, there are a few scenarios that need to be accounted for:

- `influxdb-relay` will not relay the `/query` endpoint, and this includes schema modification (create database, `DROP`s, etc). This means that databases must be created before points are written to the backends.
- Continuous queries will still only write their results locally. If a server goes down, the continuous query will have to be backfilled after the data has been recovered for that instance.
- Overwriting points is potentially unpredictable. For example, given servers A and B, if B is down, and point X is written (we'll call the value X1) just before B comes back online, that write is queued behind every other write that occurred while B was offline. Once B is back online, the first buffered write succeeds, and all new writes are now allowed to pass-through. At this point (before X1 is written to B), X is written again (with value X2 this time) to both A and B. When the relay reaches the end of B's buffered writes, it will write X (with value X1) to B... At this point A now has X2, but B has X1.
  - It is probably best to avoid re-writing points (if possible). Otherwise, please be aware that overwriting the same field for a given point can lead to data differences.
  - This could potentially be mitigated by waiting for the buffer to flush before opening writes back up to being passed-through.

## Building

The recommended method for building `influxdb-relay` is to use Docker
and the included `Dockerfile_build_ubuntu64` Dockerfile, which
includes all of the necessary dependencies.

To build the docker image, you can run:

```
docker build -f Dockerfile_build_ubuntu64 -t influxdb-relay-builder:latest .
```

And then to build the project:

```
docker run --rm -v $(pwd):/root/go/src/github.com/influxdata/influxdb-relay influxdb-relay-builder
```

*NOTE* By default, builds will be for AMD64 Linux (since the container
is running AMD64 Linux), but to change the target platform or
architecture, use the `--platform` and `--arch` CLI options.

Which should immediately call the included `build.py` build script,
and leave any build output in the `./build` directory. To see a list
of available build commands, append a `--help` to the command above.

```
docker run -v $(pwd):/root/go/src/github.com/influxdata/influxdb-relay influxdb-relay-builder --help
```

### Packages

To build system packages for Linux (`deb`, `rpm`, etc), use the
`--package` option:

```
docker run -v $(pwd):/root/go/src/github.com/influxdata/influxdb-relay influxdb-relay-builder --package
```

To build packages for other platforms or architectures, use the
`--platform` and `--arch` options. For example, to build an amd64
package for Mac OS X, use the options `--package --platform darwin`.

