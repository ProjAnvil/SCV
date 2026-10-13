package com.acme.web;

import com.acme.core.GreetingAudit;
import org.springframework.web.bind.annotation.ControllerAdvice;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.ResponseBody;

// torture: @ControllerAdvice stereotype + constructor injection
@ControllerAdvice
public class GlobalExceptionHandler {

    private final GreetingAudit audit;

    public GlobalExceptionHandler(GreetingAudit audit) {
        this.audit = audit;
    }

    @ExceptionHandler(IllegalArgumentException.class)
    @ResponseBody
    public String handle(IllegalArgumentException e) {
        audit.audit("handled " + e.getMessage());
        return "bad request";
    }
}
