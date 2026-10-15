package com.sb.ecp;

import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.boot.context.properties.EnableConfigurationProperties;
import org.springframework.context.annotation.Configuration;
import org.springframework.stereotype.Component;

// §5: @ConfigurationProperties record registered via
// @EnableConfigurationProperties(X.class) (syntactic .class match), and
// record-component constructor injection.
@ConfigurationProperties(prefix = "feature")
record FeatureProps(String label) {
}

@Configuration
@EnableConfigurationProperties(FeatureProps.class)
class EcpConfig {
}

interface Pay {
}

@Component
record CardChannel(FeatureProps props) implements Pay {
}
