package com.sb.value;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Component;

// §5: @Value params are skipped (no edge, no unresolved entry).
@Component
class Sink {
}

@Component
class ValueConsumer {

    ValueConsumer(@Value("${cfg.name}") String name, Sink sink) {
    }
}
