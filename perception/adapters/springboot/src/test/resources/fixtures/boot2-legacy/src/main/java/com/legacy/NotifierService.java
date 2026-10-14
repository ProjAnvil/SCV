package com.legacy;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

import javax.annotation.PostConstruct;
import java.time.Clock;

// legacy: TWO constructors, only the @Autowired one counts; javax lifecycle
@Service
public class NotifierService {

    private final FancyRepository repository;
    private final Clock clock;

    public NotifierService() {
        this.repository = null;
        this.clock = null;
    }

    @Autowired
    public NotifierService(FancyRepository repository, Clock clock) {
        this.repository = repository;
        this.clock = clock;
    }

    @PostConstruct
    void init() {
    }
}
