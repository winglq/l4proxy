# l4proxy

## Description

l4proxy is used to export services on non static ip hosts by a public host. For example you can export sshd/vnc/3389 service in your homelab by a vps using l4proxy.

## Features

* l4 reverse proxy.
* API to list connected clients and backend users.
* load balance for same backend service on different host.
* l7 forwarder.
* connect backend user and backend directly (STUN like).

## Architecture

The following is how different components are connected together. SSH service is used as an example.

```
+-backend user-+      +---public host------+   +------protected host 1----------+ 
| +----------+ |      | +--------------+   |   |   +--------------+      +----+ |
| |ssh client| | <--> | |l4proxy server| <-+-+-+-> |l4proxy client| <--> |sshd| |
| +----------+ |      | +--------------+   | | |   +--------------+      +----+ |
+--------------+      +--------------------+ | +--------------------------------+ 
                                             | +------protected host 2----------+
                                             | |   +--------------+      +----+ |
                                             +-+   |l4proxy client| <--> |sshd| |
                                               |   +--------------+      +----+ |
                                               +--------------------------------+
```

One (backend user <--> l4proxy server) connection is mapped to one (l4proxy server <--> l4proxy client) connection.
Each client will connect to a seperate internal socket.
A public control port is required, and two addition ports for each client.

## Build

```
git clone https://github.com/winglq/l4proxy
cd l4proxy
make bin # linux
make win # windows
```

## Usage

Create l4proxy server. Run the following command on vps.

```
l4proxy server --host 1.2.3.4
```

Create l4proxy client. Run the following command on homelab servers.

```
l4proxy client --svr_addr 1.2.3.4 127.0.0.1 22
```

Use --int_port and --pub_port in l4proxy client to specify the listen port on l4proxy server, make sure a tcp rule is added to the firewall to allow connection on these ports.
If you want to connect backend user and backend directly, you --backend_port to specify the port that was manually mapped on your home router. l4proxy server will try to connect the port directly if connection is established, the backend's public network address and backend port is returned to client, otherwise the server fallback to a proxy server.

For more detail usage use `l4proxy -h`.

## To do

1. unit tests reqired.
2. CI.
3. integration tests.
4. shared internal port between different clients.
5. error handling.
6. rate limit support
7. web ?
8. l7 forwarder client and service closing is working in progress 
9. will add a real port map or unmap implementation(like openwrt) to make STUN easy.
