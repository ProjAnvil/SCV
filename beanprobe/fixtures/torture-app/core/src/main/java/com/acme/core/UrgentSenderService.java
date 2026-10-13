package com.acme.core;

import org.springframework.stereotype.Service;

// torture: unqualified single-point injection with a @Primary candidate present
// -> expects exactly ONE edge, to smsGreetingSender (the @Primary bean)
@Service
public class UrgentSenderService {

    private final GreetingSender sender;

    public UrgentSenderService(GreetingSender sender) {
        this.sender = sender;
    }

    public void notify(String message) {
        sender.send(message);
    }
}
