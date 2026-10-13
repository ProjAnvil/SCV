package com.acme.core;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Service;

// torture: TWO constructors, only one @Autowired-annotated.
// Spring uses the @Autowired one; the other must NOT contribute injection edges.
@Service
public class MultiCtorService {

    private final GreetingAudit audit;
    private final CacheWarmer warmer;

    @Autowired
    public MultiCtorService(GreetingAudit audit) {
        this.audit = audit;
        this.warmer = null;
    }

    public MultiCtorService(GreetingAudit audit, CacheWarmer warmer) {
        this.audit = audit;
        this.warmer = warmer;
    }

    public void run() {
        audit.audit("multi-ctor");
        if (warmer != null) warmer.warm();
    }
}
