package com.acme;

import org.springframework.stereotype.Component;

// torture: injects the conditional SchedulingPolicy — expects an honest
// ambiguous pair of edges to defaultPolicy + customPolicy
@Component
public class ExecutorInspector {

    private final SchedulingPolicy policy;

    public ExecutorInspector(SchedulingPolicy policy) {
        this.policy = policy;
    }

    public int report() {
        return policy.parallelism();
    }
}
