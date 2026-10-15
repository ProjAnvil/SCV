package com.sb.ctor;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.stereotype.Component;

import java.time.Clock;

// §5 multi-ctor selection: the @Autowired-annotated constructor wins; the
// other contributes no injection points.
@Component
class Sink {
}

@Configuration
class McConfig {

    @Bean
    public Clock clock() {
        return Clock.systemUTC();
    }
}

@Component
class MultiCtor {

    @Autowired
    MultiCtor(Sink sink) {
    }

    MultiCtor(Sink sink, Clock clock) {
    }
}
