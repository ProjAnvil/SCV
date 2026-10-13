package com.acme.web;

import com.acme.core.GreetingSender;
import com.acme.core.GreetingService;
import org.springframework.beans.factory.annotation.Qualifier;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RequestParam;

// torture: custom meta-annotation stereotype + ctor-param @Qualifier,
// cross-module dependency (web -> core)
@MyRestController
public class GreetingController {

    private final GreetingService greetingService;
    private final GreetingSender urgentChannel;

    public GreetingController(GreetingService greetingService,
                              @Qualifier("emailGreetingSender") GreetingSender urgentChannel) {
        this.greetingService = greetingService;
        this.urgentChannel = urgentChannel;
    }

    @GetMapping("/greet")
    public String greet(@RequestParam String name) {
        urgentChannel.send("greeting requested");
        return greetingService.greet(name);
    }

    // torture: nested static @Component — a bean inside another bean's file
    @org.springframework.stereotype.Component
    public static class Metrics {
        private final com.acme.core.GreetingAudit audit;

        public Metrics(com.acme.core.GreetingAudit audit) {
            this.audit = audit;
        }

        public void count() {
            audit.audit("request counted");
        }
    }
}
