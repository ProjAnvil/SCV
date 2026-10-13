package com.acme.core;

import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

import java.util.List;

// torture: Lombok ctor + List<T> collection injection (expects ALL beans of T)
@Service
@RequiredArgsConstructor
public class BroadcastService {

    private final List<GreetingSender> senders;
    private final GreetingAudit audit;

    public void broadcast(String message) {
        audit.audit("broadcast");
        senders.forEach(s -> s.send(message));
    }
}
