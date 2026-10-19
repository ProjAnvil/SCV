package com.acme.order.config;

import com.acme.order.client.Mailer;
import com.acme.order.client.RateClient;
import com.acme.order.client.SmtpMailer;
import org.springframework.boot.autoconfigure.condition.ConditionalOnMissingBean;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Profile;

import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.function.Function;

/**
 * @Bean-method injection plus conditional assembly. The conditional
 * annotations must land in the bean's `conditions` property so SCV can flag
 * them instead of guessing their truth value.
 */
@Configuration
@EnableConfigurationProperties(AcmeProperties.class)
public class InfrastructureConfig {

    @Bean
    @ConditionalOnMissingBean
    public Mailer defaultMailer() {
        return new SmtpMailer();
    }

    @Bean("gateway")
    @ConditionalOnProperty(prefix = "acme.gateway", name = "enabled", havingValue = "true")
    public Function<String, Boolean> gateway(AcmeProperties properties) {
        return orderId -> properties.isGatewayEnabled() && orderId != null;
    }

    @Bean
    @Profile({"prod", "staging"})
    public ExecutorService orderExecutor(AcmeProperties properties) {
        return Executors.newFixedThreadPool(properties.getWorkerThreads());
    }

    @Bean
    public RateClient cachingRateClient(RateClient delegate) {
        return customerId -> delegate.rateFor(customerId);
    }
}
