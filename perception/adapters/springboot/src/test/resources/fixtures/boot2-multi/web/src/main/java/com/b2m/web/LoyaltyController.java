package com.b2m.web;

import com.b2m.service.LoyaltyService;
import org.springframework.stereotype.Controller;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.ResponseBody;

@Controller
public class LoyaltyController {

    private final LoyaltyService loyaltyService;

    public LoyaltyController(LoyaltyService loyaltyService) {
        this.loyaltyService = loyaltyService;
    }

    @GetMapping("/loyalty/{id}")
    @ResponseBody
    public long loyalty(@PathVariable long id) {
        return loyaltyService.points(id);
    }
}
