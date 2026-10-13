package com.example.demo;

import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.stereotype.Component;

@Component
@ConditionalOnProperty(name = "audit.enabled", havingValue = "true")
public class AuditService {
    public void audit(String event) {
        System.out.println("[audit] " + event);
    }
}
