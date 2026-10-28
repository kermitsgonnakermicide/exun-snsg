#include <Arduino.h>
#include <Wire.h>
#include <Arduino_GFX_Library.h>
#include "eeprom_data.h"
//shittiest fucking code ive ever written what the fuck is any of this ahhhhh
// handcodin in th ebig '26
constexpr uint16_t B=0, W=0xFFFF, Y=0xFFE0, G=0x03E0;
Arduino_DataBus *bus=new Arduino_ESP32SPI(4,5,6,7,GFX_NOT_DEFINED,FSPI);
Arduino_GFX *gfx=new Arduino_ST7789(bus,8,0,true,240,280,0,20);
const char *modes[]={"I2C EEPROM","SPI EEPROM","Parallel"};
uint32_t sizes[6];
uint8_t mode=0, size=0, count=0, page=0;
bool drop=false;
uint32_t addr=0;

void setSizes(){
  const uint32_t base[]={1024,4096,8192,16384,32768};
  for(uint32_t s:base)if(s<eepromSize)sizes[count++]=s;
  if(!count||sizes[count-1]!=eepromSize)sizes[count++]=eepromSize;
}
void label(uint8_t i,char *s){
  if(sizes[i]%1024==0)snprintf(s,16,"%lu KB",(unsigned long)(sizes[i]/1024));
  else snprintf(s,16,"%lu B",(unsigned long)sizes[i]);
}

bool touch(int &x,int &y){
  Wire.beginTransmission(0x15); Wire.write(2);
  if(Wire.endTransmission()||Wire.requestFrom(0x15,(uint8_t)5)!=5)return false;
  uint8_t f=Wire.read(),xh=Wire.read(),xl=Wire.read(),yh=Wire.read(),yl=Wire.read();
  if(!f)return false;
  x=((xh&15)<<8)|xl; y=((yh&15)<<8)|yl; return true;
}
void button(int y,const char *s,uint16_t c=W){gfx->drawRect(12,y,216,38,c);gfx->setCursor(24,y+12);gfx->setTextColor(c);gfx->print(s);}
void draw(){
  gfx->fillScreen(B); gfx->setTextColor(W); gfx->setTextSize(2); gfx->setCursor(12,12); gfx->print("EEPROM DUMP");
  if(page==0){
    for(int i=0;i<3;i++){gfx->drawRect(12,48+i*48,216,40,mode==i?Y:W);gfx->setCursor(24,60+i*48);gfx->setTextColor(W);gfx->print(modes[i]);}
    gfx->fillRect(12,200,216,48,G);gfx->setTextColor(B);gfx->setCursor(68,216);gfx->print("DUMP");
  }else if(page==1){
    gfx->setTextColor(W);gfx->setCursor(12,52);gfx->print(modes[mode]);
    char s[16];label(size,s);button(80,s,Y);
    if(drop)for(int i=0;i<count;i++){label(i,s);gfx->drawRect(12,112+i*22,216,22,i==size?Y:W);gfx->setTextSize(1);gfx->setCursor(24,119+i*22);gfx->setTextColor(W);gfx->print(s);}
    else {gfx->fillRect(12,204,216,48,G);gfx->setTextColor(B);gfx->setTextSize(2);gfx->setCursor(47,220);gfx->print("CONFIRM");}
  }else if(page==2){
    gfx->setTextSize(1);gfx->setCursor(12,72);gfx->printf("Address: %lu / %lu",(unsigned long)addr,(unsigned long)sizes[size]);
    gfx->drawRect(12,110,216,28,W);gfx->fillRect(14,112,(uint32_t)214*addr/sizes[size],24,G);
    gfx->setCursor(12,154);gfx->printf("%lu%%",(unsigned long)(100UL*addr/sizes[size]));
  }else{gfx->setCursor(12,88);gfx->print("Stream complete");gfx->fillRect(12,180,216,48,G);gfx->setTextColor(B);gfx->setCursor(76,196);gfx->print("DONE");}
}
void setup(){
  Serial.begin(115200);
  setSizes();
  Wire.begin(11,10); pinMode(15,OUTPUT);digitalWrite(15,HIGH);pinMode(13,OUTPUT);digitalWrite(13,HIGH);delay(10);digitalWrite(13,LOW);delay(10);digitalWrite(13,HIGH);
  gfx->begin();gfx->setRotation(0);draw();
}
void loop(){
  if(page==2){
    uint8_t b[128];uint32_t n=min((uint32_t)sizeof(b),sizes[size]-addr);
    for(uint32_t i=0;i<n;i++)b[i]=pgm_read_byte(eepromData+addr+i);
    Serial.write(b,n);addr+=n;
    if(addr==sizes[size]){Serial.flush();page=3;}
    draw();delay(10);
    return;
  }
  int x,y;if(!touch(x,y)||x>=240||y>=280)return;
  if(page==0){
    if(y>=48&&y<192)mode=(y-48)/48;
    else if(y>=200&&y<248)page=1;
  }else if(page==1){
    if(drop&&y>=112&&y<112+count*22){size=(y-112)/22;drop=false;}
    else if(y>=80&&y<118)drop=true;
    else if(!drop&&y>=204&&y<252){addr=0;page=2;}
  }else if(page==3&&y>=180&&y<240)page=0;
  draw();delay(250);
}
