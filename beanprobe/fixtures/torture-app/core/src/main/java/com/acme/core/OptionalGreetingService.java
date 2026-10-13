package com.acme.core;

import org.springframework.stereotype.Service;

import java.util.Optional;

// torture: Optional<T> injection — unwrap to CacheWarmer
@Service
public class OptionalGreetingService {

    private final Optional<CacheWarmer> warmer;

    public OptionalGreetingService(Optional<CacheWarmer> warmer) {
        this.warmer = warmer;
    }

    public void maybeWarm() {
        warmer.ifPresent(CacheWarmer::warm);
    }
}
