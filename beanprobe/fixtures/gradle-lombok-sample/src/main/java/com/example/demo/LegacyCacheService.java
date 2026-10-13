package com.example.demo;

import org.springframework.context.annotation.Profile;
import org.springframework.stereotype.Component;

@Component
@Profile("legacy")
public class LegacyCacheService {
    public void warmup() {
        System.out.println("[legacy-cache] warmup");
    }
}
