# InfluxDB Relay

This project adds a basic high availability layer to InfluxDB. With the right architecture and disaster recovery processes, this achieves a highly available setup. It will be available with the 0.12.0 release of InfluxDB in April 2016.

## Usage

To build from source and run:

```sh
$ # Install influxdb-relay to your $GOPATH/bin
$ go install github.com/influxdata/influxdb-relay
$ # Edit your configuration file
$ cp $GOPATH/github.com/influxdata/influxdb-relay/sample.toml ./relay.toml
$ vim relay.toml
$ # Start relay!
$ $GOPATH/influxdb-relay -config relay.toml
```

## Configuration

```toml
[[http]]
# Name of the HTTP server, used for display purposes only.
name = "example-http"

# TCP address to bind to, for HTTP server.
bind-addr = "127.0.0.1:9096"

# Array of InfluxDB instances to use as backends for Relay.
output = [
    # name: name of the backend, used for display purposes only.
    # location: full URL of the /write endpoint of the backend
    # timeout: Go-parseable time duration. Fail writes if incomplete in this time.
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


The relay will listen for HTTP or UDP writes and write the data to both servers via their HTTP write endpoint. If the write is sent via HTTP, the relay will return a success response as soon as one of the two InfluxDB servers returns a success. If either InfluxDB server returns a 400 response, that will be returned to the client immediately. If both servers return a 500, a 500 will be returned to the client.

With this setup a failure of one Relay or one InfluxDB can be sustained while still taking writes and serving queries. However, the recovery process will require operator intervention.

## Recovery

InfluxDB organizes its data on disk into logical blocks of time called shards. We can use this to create a hot recovery process with zero downtime.

The length of time that shards represent in InfluxDB range from 1 hour to 7 days. For retention policies with an infinite duration (that is they keep data forever), their shard durations are 7 days. For the sake of our example, let's assume shard sizes of 1 day.

Let's say one of the InfluxDB servers goes down for an hour on 2016-03-10. Once the next day rolls over and we're now writing data to 2016-03-11, we can then restore things using these steps:

1. Create backup of 2016-03-10 shard from server that was up the entire day
2. Tell the load balancer to stop sending query traffic to the server that was down
3. Restore the backup of the shard from the good server to the old server
4. Tell the load balancer to resume sending queries to the previously downed server

During this entire process the Relays should be sending writes to both servers for the current shard (2016-03-11).

## Sharding

It's possible to add another layer on top of this kind of setup to shard data. Depending on your needs you could shard on the measurement name or a specific tag like `customer_id`. The sharding layer would have to service both queries and writes.

## Buffering

The relay can be configured to buffer failed requests.
The intent of this logic is reduce the number of failures during short outages or periodic network issues.
> This retry logic is **NOT** sufficient for for long periods of downtime as all data is buffered in RAM

Buffering has two configuration options:

* BufferSize -- the maximum number of requests to buffer per backend.
* MaxDelayInterval -- the max delay between retry attempts per backend.
    The initial retry interval is 500ms and is doubled after every failure.

If the buffer is full then requests are dropped and an error is logged.
If a requests makes it into the buffer it is retried until success.

Retries are serialized to a single backend.
Meaning that buffered requests are attempted one at a time.
If buffered requests succeed then there is no delay between subsequent attempts.
