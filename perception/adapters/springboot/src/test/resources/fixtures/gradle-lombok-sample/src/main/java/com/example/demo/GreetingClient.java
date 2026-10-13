package com.example.demo;

import java.time.Clock;

public class GreetingClient {

    private final GreetingService greetingService;
    private final Clock clock;

    public GreetingClient(GreetingService greetingService, Clock clock) {
        this.greetingService = greetingService;
        this.clock = clock;
    }

    public String timedGreet(String name) {
        long start = clock.millis();
        String result = greetingService.greet(name);
        System.out.println("took " + (clock.millis() - start) + "ms");
        return result;
    }
}
