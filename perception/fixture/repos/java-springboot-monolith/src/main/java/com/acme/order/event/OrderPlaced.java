package com.acme.order.event;

import java.time.Instant;

public record OrderPlaced(String orderId, String customerId, Instant occurredAt) {
}
