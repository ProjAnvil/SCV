package com.b2m.web;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;

// the boot entry point lives in the WEB SUBMODULE, not the aggregator root —
// exercises multi-module detection confirmation
@SpringBootApplication
public class Boot2MultiApplication {
    public static void main(String[] args) {
        SpringApplication.run(Boot2MultiApplication.class, args);
    }
}
