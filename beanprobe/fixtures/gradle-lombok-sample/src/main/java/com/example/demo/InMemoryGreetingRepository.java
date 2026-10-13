package com.example.demo;

import org.springframework.stereotype.Repository;

import java.util.concurrent.ConcurrentHashMap;

@Repository
public class InMemoryGreetingRepository implements GreetingRepository {

    private final ConcurrentHashMap<String, String> templates = new ConcurrentHashMap<>();

    public InMemoryGreetingRepository() {
        templates.put("default", "Hello, %s!");
    }

    @Override
    public String loadTemplate() {
        return templates.getOrDefault("default", "Hi %s");
    }
}
