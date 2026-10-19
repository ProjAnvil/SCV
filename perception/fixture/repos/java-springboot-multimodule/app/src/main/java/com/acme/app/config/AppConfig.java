package com.acme.app.config;

import com.acme.infra.client.LedgerClient;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class AppConfig {

    /** @Bean method with a cross-module parameter type. */
    @Bean
    public LedgerClient ledgerClient() {
        return new LedgerClient();
    }
}
