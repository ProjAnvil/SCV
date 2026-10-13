package com.acme.core;

import org.springframework.boot.context.properties.ConfigurationProperties;

// torture: NO stereotype annotation — registered only via
// @EnableConfigurationProperties(FeatureProperties.class) in the app module.
// A pure stereotype scanner misses this; the analyzer must follow the registration.
@ConfigurationProperties(prefix = "feature")
public class FeatureProperties {
    private boolean verbose = false;

    public boolean isVerbose() {
        return verbose;
    }

    public void setVerbose(boolean verbose) {
        this.verbose = verbose;
    }
}
