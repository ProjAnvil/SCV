package com.acme.core;

import org.springframework.stereotype.Component;

@Component
public record TemplateEngine(GreetingProperties properties) {
    public String render(String name) {
        return properties.getTemplate().replace("%s", name);
    }
}
