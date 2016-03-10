# InfluxDB Relay

This project adds a basic high availability layer to InfluxDB. With the right architecture and disaster recovery processes, this achieves a highly available setup. It will be available with the 0.12.0 release of InfluxDB in April 2016.

## Description

The following is a proposed architecture for the project. This may get more fleshed out and fully featured as it develops as part of the InfluxDB 0.12.0 development cycle.

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
│  └─────┬────┘      └────┬─────┘  │        
│     ┌──┼────────────────┤        │
│     │  ├────────────────┼──┐     │        
│     │  │                │  │     │        
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
