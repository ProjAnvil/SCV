package com.b2m.web;

import com.b2m.service.CustomerService;
import org.springframework.stereotype.Controller;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.ResponseBody;

@Controller
public class CustomerController {

    private final CustomerService customerService;

    public CustomerController(CustomerService customerService) {
        this.customerService = customerService;
    }

    @GetMapping("/customers/{id}")
    @ResponseBody
    public String customer(@PathVariable long id) {
        return customerService.describe(id);
    }
}
