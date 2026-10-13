package com.acme.core;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;

// torture: constructor mixing a @Value param with a real bean param
@Repository
public class JpaGreetingRepository implements GreetingRepository {

    private final GreetingAudit audit;
    private final String datasourceUrl;

    public JpaGreetingRepository(@Value("${greeting.datasource.url}") String datasourceUrl, GreetingAudit audit) {
        this.datasourceUrl = datasourceUrl;
        this.audit = audit;
    }

    @Override
    public String loadTemplate() {
        audit.audit("load template via " + datasourceUrl);
        return "Hello, %s!";
    }
}
