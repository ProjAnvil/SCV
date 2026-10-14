package com.legacy;

import org.springframework.beans.factory.annotation.Autowired;

// legacy style: @Autowired service in an abstract base, subclass is the bean
public abstract class AbstractBaseController {

    @Autowired
    protected FancyRepository repository;
}
