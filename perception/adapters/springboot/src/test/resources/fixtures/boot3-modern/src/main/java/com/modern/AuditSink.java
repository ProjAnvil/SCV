package com.modern;

import org.springframework.scheduling.annotation.Scheduled;
import org.springframework.stereotype.Component;

@Component
public class AuditSink {

    @Scheduled(fixedDelay = 60_000)
    void flush() {
    }

    void record(String event) {
    }
}
