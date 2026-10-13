package com.acme.core;

import org.springframework.stereotype.Component;

// torture: decapitalize edge case — two leading uppercase letters keep the name as-is
@Component
public class URLValidator {
    public boolean valid(String url) {
        return url != null && url.startsWith("https://");
    }
}
