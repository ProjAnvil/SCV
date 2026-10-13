package com.acme.core;

import org.springframework.stereotype.Service;

// torture: injection points inherited from abstract base + own constructor
@Service
public class GreetingServiceImpl extends AbstractAuditedService implements GreetingService {

    private final GreetingRepository repository;

    public GreetingServiceImpl(GreetingRepository repository) {
        this.repository = repository;
    }

    @Override
    public String greet(String name) {
        audit.audit("greet " + name);
        return repository.loadTemplate().replace("%s", name);
    }
}
