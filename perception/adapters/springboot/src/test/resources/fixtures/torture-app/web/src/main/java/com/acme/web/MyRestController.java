package com.acme.web;

import org.springframework.core.annotation.AliasFor;
import org.springframework.transaction.annotation.Transactional;
import org.springframework.web.bind.annotation.RestController;

import java.lang.annotation.ElementType;
import java.lang.annotation.Retention;
import java.lang.annotation.RetentionPolicy;
import java.lang.annotation.Target;

// torture: SOURCE-LEVEL custom stereotype two levels deep:
// @MyRestController -> @RestController (stereotype) and @Transactional (noise)
@Target(ElementType.TYPE)
@Retention(RetentionPolicy.RUNTIME)
@RestController
@Transactional
public @interface MyRestController {
    @AliasFor(annotation = RestController.class)
    String value() default "";
}
