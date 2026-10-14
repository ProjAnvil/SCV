package com.legacy;

import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.stereotype.Controller;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.ResponseBody;

@Controller
public class OrderController extends AbstractBaseController {

    @Autowired
    private NotificationChannel channel; // two impls, no qualifier -> ambiguous

    @GetMapping("/orders")
    @ResponseBody
    public String orders() {
        return repository.name() + ":" + channel.send("list");
    }
}
