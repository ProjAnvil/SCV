package com.modern;

import org.springframework.boot.context.properties.ConfigurationProperties;

// Boot 3 idiom: record @ConfigurationProperties, constructor binding
@ConfigurationProperties(prefix = "pay")
public record PaymentProps(String label) {
    public PaymentProps {
        if (label == null) {
            label = "default";
        }
    }
}
