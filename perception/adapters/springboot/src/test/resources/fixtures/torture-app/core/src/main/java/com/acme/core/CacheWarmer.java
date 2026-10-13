package com.acme.core;

import org.springframework.stereotype.Component;

@Component
public class CacheWarmer {
    public void warm() {
        System.out.println("[cache] warm");
    }
}
