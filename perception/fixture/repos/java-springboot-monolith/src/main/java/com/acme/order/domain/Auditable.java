package com.acme.order.domain;

import java.time.Instant;

/** Marker interface with a default method: exercises interface default handling. */
public interface Auditable {

    Instant auditedAt();

    default boolean recentlyAudited(Instant now) {
        return auditedAt() != null && auditedAt().plusSeconds(3600).isAfter(now);
    }
}
