package com.acme.core;

import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

// control: detected via @Component; properties payload is irrelevant to the bean graph
@Component
@ConfigurationProperties(prefix = "greeting")
public class GreetingProperties {
    private String template = "Hello, %s!";

    public String getTemplate() {
        return template;
    }

    public void setTemplate(String template) {
        this.template = template;
    }
}
