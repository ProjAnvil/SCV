package com.acme.order.domain;

import java.time.Instant;
import java.util.List;
import java.util.Objects;

/** Aggregate root. Record used on purpose: exercises record support. */
public record Order(
        String id,
        String customerId,
        List<OrderLine> lines,
        OrderStatus status,
        Instant createdAt) implements Auditable {

    public Order {
        Objects.requireNonNull(id, "id");
        lines = lines == null ? List.of() : List.copyOf(lines);
    }

    public long totalCents() {
        return lines.stream().mapToLong(OrderLine::subtotalCents).sum();
    }

    public boolean isSettled() {
        return status == OrderStatus.SETTLED || status == OrderStatus.REFUNDED;
    }

    @Override
    public Instant auditedAt() {
        return createdAt;
    }
}
