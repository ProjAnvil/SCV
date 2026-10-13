package com.acme.core;

import org.springframework.beans.factory.annotation.Autowired;

// torture: @Autowired field declared in an ABSTRACT SUPERCLASS — the bean is the subclass
public abstract class AbstractAuditedService {

    @Autowired
    protected GreetingAudit audit;
}
