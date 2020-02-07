# l4proxy

## Description

l4proxy is used to export services on non static ip hosts by a public host. For example you can export sshd/vnc/3389 service in your homelab by a vps using l4proxy.

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

Create l4proxy server.

```
l4proxy server --host your_ip
```

Create l4proxy client.

```
l4proxy client --svr_addr your_ip 127.0.0.1 backend_service_port
```

Use --int_port and --pub_port in l4proxy client to specify the listen port on l4proxy server, make sure a tcp rule is added to the firewall to allow connection on these ports.

For more detail usage use `l4proxy -h`.

## To do

1. unit tests reqired.
2. CI.
3. integration tests.
4. shared internal and shared public port between different clients.
5. error handling.
6. rate limit support
7. CRUD support for connection infos.
