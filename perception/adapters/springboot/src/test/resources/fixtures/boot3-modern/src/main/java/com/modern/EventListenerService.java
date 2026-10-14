package com.modern;

import jakarta.annotation.PostConstruct;
import org.springframework.context.ApplicationEventPublisher;
import org.springframework.stereotype.Component;

@Component
public class EventListenerService {

    private final AuditSink audit;

    public EventListenerService(AuditSink audit) {
        this.audit = audit;
    }

    @PostConstruct
    void init() {
    }

    public void publish(ApplicationEventPublisher publisher) {
        publisher.publishEvent("started");
    }
}
