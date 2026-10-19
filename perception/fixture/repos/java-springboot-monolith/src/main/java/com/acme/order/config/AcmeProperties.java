package com.acme.order.config;

import org.springframework.boot.context.properties.ConfigurationProperties;

/** Nested @ConfigurationProperties: exercises the Configuration stereotype. */
@ConfigurationProperties(prefix = "acme")
public class AcmeProperties {

    private boolean gatewayEnabled = true;
    private int workerThreads = 4;
    private final Mail mail = new Mail();
    private final Gateway gateway = new Gateway();

    public boolean isGatewayEnabled() {
        return gatewayEnabled;
    }

    public void setGatewayEnabled(boolean gatewayEnabled) {
        this.gatewayEnabled = gatewayEnabled;
    }

    public int getWorkerThreads() {
        return workerThreads;
    }

    public void setWorkerThreads(int workerThreads) {
        this.workerThreads = workerThreads;
    }

    public Mail getMail() {
        return mail;
    }

    public Gateway getGateway() {
        return gateway;
    }

    public static class Mail {
        private String sender = "noreply@acme.test";
        private int timeoutMs = 2000;

        public String getSender() {
            return sender;
        }

        public void setSender(String sender) {
            this.sender = sender;
        }

        public int getTimeoutMs() {
            return timeoutMs;
        }

        public void setTimeoutMs(int timeoutMs) {
            this.timeoutMs = timeoutMs;
        }
    }

    public static class Gateway {
        private String baseUrl = "https://gateway.acme.test";
        private int retries = 2;

        public String getBaseUrl() {
            return baseUrl;
        }

        public void setBaseUrl(String baseUrl) {
            this.baseUrl = baseUrl;
        }

        public int getRetries() {
            return retries;
        }

        public void setRetries(int retries) {
            this.retries = retries;
        }
    }
}
