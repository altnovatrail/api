#!/usr/bin/env bash
# Runs on the host — the machine VS Code itself runs on — before the container starts.
#
# Docker hands its networks an MTU of 1500. When the host reaches the internet through a
# tunnel, which in practice means a VPN, the real limit is lower, and packets that fit inside
# the container are dropped on the way out with nothing to say so: small requests answer,
# large downloads stop after a few kilobytes, and the failure looks like anything but the
# network. Giving the container a network built with the host's own MTU settles it, and asks
# nothing of whoever opens this repository.

set -euo pipefail

network="novatrail-api"

# The interface that traffic bound for the internet leaves by, as the kernel picks it. The
# default route of the main table is not enough: a VPN often leaves it in place and routes
# around it, through a table of its own behind a policy rule (wg-quick) or through two /1
# routes (OpenVPN's def1), and `ip route get` follows both. Any address on the internet will
# do, since only the route to it is looked up and nothing is sent. Absent on hosts without
# iproute2 — macOS, for one — and then the Docker default is as good a guess as any.
mtu=1500
interface="$(ip -o route get 1.1.1.1 2> /dev/null \
    | awk '{ for (i = 1; i < NF; i++) if ($i == "dev") { print $(i + 1); exit } }' || true)"
if [[ -n "$interface" && -r "/sys/class/net/${interface}/mtu" ]]; then
    detected="$(cat "/sys/class/net/${interface}/mtu")"
    # Only ever lowered: a larger one — jumbo frames on the local network — says nothing
    # about the path to the internet.
    mtu=$(( detected < mtu ? detected : mtu ))
fi

if ! docker network inspect "$network" > /dev/null 2>&1; then
    docker network create --opt "com.docker.network.driver.mtu=${mtu}" "$network" > /dev/null
    echo "network ${network}: created with mtu ${mtu}"
    exit 0
fi

# A network made without the option — by hand, say — runs at Docker's default.
existing="$(docker network inspect "$network" \
    --format '{{ index .Options "com.docker.network.driver.mtu" }}')"
existing="${existing:-1500}"

if [[ "$existing" != "$mtu" ]]; then
    # A network in use cannot be changed, and taking it down from here would take the
    # container with it.
    echo "network ${network} has mtu ${existing}, this host now has ${mtu}." >&2
    echo "To pick the new one up: close the container, then docker network rm ${network}" >&2
fi
