package com.modern;

import org.springframework.context.annotation.Configuration;
import org.springframework.context.annotation.Import;
import org.springframework.boot.context.properties.EnableConfigurationProperties;

@Configuration
@Import(AsyncConfig.class)
@EnableConfigurationProperties(PaymentProps.class)
public class AppConfig {
}
