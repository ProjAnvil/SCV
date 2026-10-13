package com.acme.core;

import org.springframework.stereotype.Component;

@Component
public class GreetingAudit {
    public void audit(String event) {
        System.out.println("[audit] " + event);
    }
}
