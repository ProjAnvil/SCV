package com.acme;

import com.acme.core.GreetingSender;

import java.time.Clock;

public class GreetingClient {

    private final GreetingSender sender;
    private final Clock clock;

    public GreetingClient(GreetingSender sender, Clock clock) {
        this.sender = sender;
        this.clock = clock;
    }

    public String sendTimed(String message) {
        sender.send(message);
        return "sent at " + clock.millis();
    }
}
