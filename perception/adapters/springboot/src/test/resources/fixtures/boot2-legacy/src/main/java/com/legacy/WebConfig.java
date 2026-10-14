package com.legacy;

import org.springframework.context.annotation.Configuration;
import org.springframework.web.servlet.config.annotation.WebMvcConfigurer;

// Boot 2 idiom: @Configuration implementing WebMvcConfigurer
@Configuration
public class WebConfig implements WebMvcConfigurer {
}
