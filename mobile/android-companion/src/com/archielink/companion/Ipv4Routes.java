package com.archielink.companion;

import java.util.ArrayList;
import java.util.List;

final class Ipv4Routes {
    private static final String[] PRIVATE_ROUTES = {
        "0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8",
        "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16", "224.0.0.0/3",
    };

    static List<String> publicRoutes(String address, int prefix) {
        List<Route> routes = new ArrayList<>();
        routes.add(parse(address, prefix));
        for (String excluded : PRIVATE_ROUTES) {
            String[] parts = excluded.split("/");
            Route block = parse(parts[0], Integer.parseInt(parts[1]));
            List<Route> next = new ArrayList<>();
            for (Route route : routes) {
                subtract(next, route, block);
            }
            routes = next;
        }
        List<String> result = new ArrayList<>();
        for (Route route : routes) {
            result.add(route.address() + "/" + route.prefix);
        }
        return result;
    }

    static boolean contains(List<String> routes, String address) {
        Route target = parse(address, 32);
        for (String value : routes) {
            String[] parts = value.split("/");
            Route route = parse(parts[0], Integer.parseInt(parts[1]));
            if ((target.base & route.mask()) == route.base) {
                return true;
            }
        }
        return false;
    }

    private static void subtract(List<Route> output, Route route, Route excluded) {
        if (!overlaps(route, excluded)) {
            output.add(route);
            return;
        }
        if (excluded.prefix <= route.prefix || route.prefix == 32) {
            return;
        }
        long half = 1L << (31 - route.prefix);
        subtract(output, new Route(route.base, route.prefix + 1), excluded);
        subtract(output, new Route(route.base + half, route.prefix + 1), excluded);
    }

    private static boolean overlaps(Route left, Route right) {
        return (left.base & left.mask()) == (right.base & left.mask())
            || (right.base & right.mask()) == (left.base & right.mask());
    }

    private static Route parse(String address, int prefix) {
        String[] octets = address.split("\\.");
        if (octets.length != 4 || prefix < 0 || prefix > 32) {
            throw new IllegalArgumentException("invalid IPv4 route");
        }
        long value = 0;
        for (String octet : octets) {
            int part = Integer.parseInt(octet);
            if (part < 0 || part > 255) {
                throw new IllegalArgumentException("invalid IPv4 route");
            }
            value = (value << 8) | part;
        }
        long mask = prefix == 0 ? 0 : (0xffffffffL << (32 - prefix)) & 0xffffffffL;
        return new Route(value & mask, prefix);
    }

    private static final class Route {
        final long base;
        final int prefix;

        Route(long base, int prefix) {
            this.base = base;
            this.prefix = prefix;
        }

        long mask() {
            return prefix == 0 ? 0 : (0xffffffffL << (32 - prefix)) & 0xffffffffL;
        }

        String address() {
            return ((base >>> 24) & 0xff) + "."
                + ((base >>> 16) & 0xff) + "."
                + ((base >>> 8) & 0xff) + "."
                + (base & 0xff);
        }
    }
}
